package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/json-iterator/go"
)

type txType uint

const (
	syncTx txType = iota
	asyncTx
	commitTx

	unconfirmedTxs = "num_unconfirmed_txs"
	getTx          = "tx"
)

func newTxType(t string) txType {
	t = strings.ToLower(t)
	t = strings.TrimSpace(t)

	txType, ok := txsNames[t]
	if !ok {
		panic("got unknown tx type " + t)
	}

	return txType
}

func (t txType) String() string {
	var s string
	switch t {
	case syncTx:
		s = "broadcast_tx_sync"
	case asyncTx:
		s = "broadcast_tx_async"
	case commitTx:
		s = "broadcast_tx_commit"
	}

	return s
}

var (
	txsNames = map[string]txType{
		"synctx":   syncTx,
		"asynctx":  asyncTx,
		"committx": commitTx,
	}

	currentTx = new(uint64)

	server   = flag.String("server", "http://localhost:26657/", "server address to fire to")
	postType = flag.String("tx", "asyncTx", "syncTx|asyncTx|commitTx")

	rps       = flag.Int("rps", 300, "syncTx|asyncTx|commitTx")
	blockTime = flag.Int("blocktime", 6, "block time in seconds")
	txSize    = flag.Int("txsize", 100, "transaction size in bytes")
	duration  = flag.Duration("duration", 10*time.Minute, "test duration in format: [0-9]*[ms,s,m,h,d,y]")
	threads   = flag.Int("threads", 50*runtime.NumCPU(), "how many threads to run")
	writeTxs  = flag.Int("writetxs", 100, "a fraction of write transactions. a non-negative number between o and 100")

	expectedBlockSize int
	txTime            time.Duration
	unconfirmedTxsNum string
	postTxURL         string
	getTxURL          string
)

func main() {
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	initParams()

	deadlineTimer := time.NewTimer(*duration)
	defer deadlineTimer.Stop()

	logFreq := (*rps) * 10
	if logFreq > 10000 {
		logFreq = 10000
	}

	fmt.Println("going to fire at rps rate", *rps)
	fmt.Println("tx size in bytes", *txSize)
	fmt.Println("fraction of write txs", *writeTxs)
	fmt.Println("txs are going to be sent by", *postType)
	fmt.Println("tx size in bytes", *txSize)
	fmt.Println("a block size is expected to be", expectedBlockSize)
	fmt.Println()

	startTime := time.Now()
	errorCount := new(uint64)

	senders := newSenderGroup(*threads, startTime, *rps, logFreq, currentTx, errorCount, int32(*writeTxs))
	senders.run()

mainLoop:
	for {
		select {
		case <-deadlineTimer.C:
			senders.stop()
			break mainLoop
		case <-shutdown:
			fmt.Println("\nstopping benchmarks...")
			senders.stop()
			break mainLoop
		default:
			//nothing to do
		}
	}

	// wait until all txs passed
	waitUnconfirmedTxs()

	totalDuration := time.Now().Sub(startTime)
	txIndex := int64(atomic.LoadUint64(currentTx))
	freq := big.NewRat(txIndex, int64(totalDuration))
	rps, _ := freq.Mul(freq, big.NewRat(int64(time.Second), 1)).Float64()

	fmt.Println()
	fmt.Println("RPS", int(rps))
	fmt.Println("Done", atomic.LoadUint64(currentTx))
	fmt.Println("Errors", atomic.LoadUint64(errorCount))
	fmt.Println("Total time", time.Now().Sub(startTime))
}

func initParams() {
	flag.Parse()

	serverURL, err := url.Parse(*server)
	if err != nil {
		panic("can't parse url: " + *server)
	}

	getURL := URL(serverURL.Scheme, serverURL.Host)

	unconfirmedTxsNum = getURL(unconfirmedTxs, "")
	postTxURL = getURL(newTxType(*postType).String(), "tx=")
	getTxURL = getURL(getTx, "hash=0x")

	expectedBlockSize = (*rps) * (*txSize) * (*blockTime)
	txTime = time.Duration(big.NewInt(0).Div(big.NewInt(int64(time.Second)), big.NewInt(int64(*rps))).Int64()) // ns

	if *writeTxs < 0 || *writeTxs > 100 {
		panic("a fraction of write transactions. a non-negative number between o and 100")
	}
}

func URL(scheme, host string) func(path, query string) string {
	return func(path, query string) string {
		return (&url.URL{Scheme: scheme, Host: host, Path: path, RawQuery: query}).String()
	}
}

func waitUnconfirmedTxs() {
	time.Sleep(50 * time.Millisecond)
	for !hasUnconfirmedTxs(false) {
		time.Sleep(50 * time.Millisecond)
	}
}

type senderGroup struct {
	senders []*sender
	cancels []chan struct{}
	state   *int32
	sync.Mutex
}

const (
	notStarted = int32(iota)
	started
	stopped
)

func newSenderGroup(n int, startTime time.Time, estimatedRPS int, logFreq int, currentTx *uint64, errorCount *uint64, writeTxProbability int32) *senderGroup {
	senders := make([]*sender, n)
	storage := new(hashStorage)

	for i := 0; i < n; i++ {
		senders[i] = newSender(startTime, estimatedRPS, logFreq, currentTx, writeTxProbability, storage, errorCount)
	}

	return &senderGroup{senders: senders, state: new(int32)}
}

func (group *senderGroup) run() {
	group.Lock()
	defer group.Unlock()

	state := atomic.LoadInt32(group.state)
	if state != notStarted {
		return
	}

	n := len(group.senders)
	group.cancels = make([]chan struct{}, n)

	for i := 0; i < n; i++ {
		group.cancels[i] = group.senders[i].run()
	}

	atomic.StoreInt32(group.state, started)
}

func (group *senderGroup) stop() {
	group.Lock()
	defer group.Unlock()

	state := atomic.LoadInt32(group.state)
	if state != started {
		return
	}

	for i := 0; i < len(group.cancels); i++ {
		close(group.cancels[i])
	}

	atomic.StoreInt32(group.state, stopped)
}

type sender struct {
	startTime          time.Time
	estimatedRPS       int
	logFreq            int
	currentTx          *uint64
	errorCount         *uint64
	writeTxProbability int32
	storage            *hashStorage

	cancel chan struct{}
}

func newSender(startTime time.Time, estimatedRPS int, logFreq int, currentTx *uint64, writeTxProbability int32, storage *hashStorage, errorCount *uint64) *sender {
	return &sender{
		startTime:          startTime,
		estimatedRPS:       estimatedRPS,
		logFreq:            logFreq,
		currentTx:          currentTx,
		writeTxProbability: writeTxProbability,
		storage:            storage,
		errorCount:         errorCount,
	}
}

func (s *sender) run() chan struct{} {
	if s.cancel != nil {
		return s.cancel
	}

	s.cancel = make(chan struct{})

	go func() {
		for {
			select {
			case <-s.cancel:
				return
			default:
				// nothing to do
			}

			s.runTx()
		}
	}()

	return s.cancel
}

func (s *sender) runTx() {
	for {
		txIndex := atomic.AddUint64(currentTx, 1)

		endTime := time.Now()
		roundTime := endTime.Sub(s.startTime)

		currentRPS := float64(txIndex) / float64(roundTime) * float64(time.Second)

		if int(currentRPS) > s.estimatedRPS {
			// we did't run any runTx so decrease Tx count
			atomic.AddUint64(currentTx, ^uint64(0))
			time.Sleep(100 * time.Millisecond)

			continue
		}

		if s.isPostTxTurn(txIndex) {
			hash, err := postTx(txIndex)
			if err != nil {
				s.onError()
				break
			}
			s.storage.storeTx(hash)
		} else {
			err := getTxByHash(s.storage.getAnyTxHash())
			if err != nil {
				s.onError()
				break
			}
		}

		log(s.startTime, txIndex, uint64(s.logFreq), s.getErrorCount())

		break
	}
}

func (s *sender) onError() {
	atomic.AddUint64(currentTx, ^uint64(0))
	s.increaseErrorCount()
	time.Sleep(100 * time.Millisecond)
}

func (s *sender) isPostTxTurn(txIndex uint64) bool {
	if !s.storage.hasAny() {
		return true
	}

	return rand.Int31n(100) < s.writeTxProbability
}

func (s *sender) increaseErrorCount() {
	atomic.AddUint64(s.errorCount, 1)
}

func (s *sender) getErrorCount() uint64 {
	return atomic.LoadUint64(s.errorCount)
}

func postTx(n uint64) (string, error) {
	tx := strconv.Itoa(time.Now().Nanosecond()) + strconv.FormatUint(n, 10)

	if len(txPrefix) == 0 {
		txPrefixLock.Lock()
		txPrefixLength := *txSize - len(tx)

		if txPrefixLength < 0 {
			txPrefixLength = 0
		}
		txPrefix = RandStringRunes(txPrefixLength)
		txPrefixLock.Unlock()
	}

	res := new(PostAsyncResponse)
	resRaw, err := doRequest(postTxURL + "\"" + txPrefix + tx + "\"")
	if err != nil {
		return "", err
	}

	decode(resRaw, res)

	return res.Res.Hash, nil
}

func getTxByHash(hash string) error {
	_, err := doRequest(getTxURL + hash)
	return err
}

const hashStorageSize = 1000

type hashStorage struct {
	hashes   [hashStorageSize]string
	index    uint64
	isFilled bool
	sync.RWMutex
}

func (s *hashStorage) storeTx(hash string) {
	s.Lock()
	defer s.Unlock()

	s.index++

	// rewrite only some indexes if storage is full
	if !s.isFilled || (s.isFilled && rand.Intn(101) < 10) {
		s.hashes[s.currentIndex()] = hash
	}

	if !s.isFilled && s.index >= hashStorageSize {
		s.isFilled = true
	}
}

func (s *hashStorage) getAnyTxHash() string {
	s.RLock()
	defer s.RUnlock()

	maxIndex := len(s.hashes)
	if !s.isFilled {
		maxIndex = s.currentIndex()
	}

	txIndex := rand.Intn(maxIndex)
	return s.hashes[txIndex]
}

func (s *hashStorage) currentIndex() int {
	return int(s.index % hashStorageSize)
}

func (s *hashStorage) hasAny() bool {
	s.RLock()
	res := s.isFilled || (!s.isFilled && s.index > 0)
	s.RUnlock()
	return res
}

var (
	letterRunes  = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	txPrefix     string
	txPrefixLock sync.RWMutex
)

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func log(mainTime time.Time, i uint64, logFreq uint64, errorCount uint64) time.Duration {
	endTime := time.Now()
	roundTime := endTime.Sub(mainTime)

	freq := big.NewRat(int64(i+1), int64(roundTime))
	rps, _ := freq.Mul(freq, big.NewRat(int64(time.Second), 1)).Float64()

	if i%logFreq != 0 {
		return roundTime
	}

	fmt.Printf("\nTotal time till Tx %d: %v. Total test duration %v. rps: %v. Errors count: %d\n",
		i, roundTime, roundTime, rps, errorCount)

	hasUnconfirmedTxs(true)

	return roundTime
}

func hasUnconfirmedTxs(withLog bool) bool {
	res, err := doRequest(unconfirmedTxsNum)
	if err != nil {
		return true
	}

	resp := new(UnconfirmedTxsResponse)
	decode(res, resp)

	if withLog {
		fmt.Println("Has Unconfirmed Txs", string(res))
	}

	n, err := strconv.Atoi(resp.Res.N)
	if err != nil {
		return true
	}

	return n == 0
}

var httpTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 0,
		DualStack: true,
	}).DialContext,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 100,
	IdleConnTimeout:     2 * time.Second,
	TLSHandshakeTimeout: 2 * time.Second,

	ExpectContinueTimeout: 1 * time.Second,
}

var httpClient = &http.Client{Transport: httpTransport}

func doRequest(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, errors.New("error while http.get:" + err.Error())
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("error while reading response body" + err.Error())
	}

	return body, nil
}

type RPCResponse struct {
	Jsonrpc string `json:"jsonrpc"`
	ID      string `json:"id"`
}

type UnconfirmedTxsResponse struct {
	RPCResponse
	Res UnconfirmedTxsResult `json:"result"`
}

type UnconfirmedTxsResult struct {
	N   string `json:"n_txs"`
	Txs *uint  `json:"txs"`
}

type PostAsyncResponse struct {
	RPCResponse
	Res PostAsyncResponseResult `json:"result"`
}

type PostAsyncResponseResult struct {
	Code string `json:"code"`
	Data string `json:"data"`
	Log  string `json:"log"`
	Hash string `json:"hash"`
}

func decode(input []byte, res interface{}) {
	var json = jsoniter.ConfigFastest
	json.Unmarshal(input, res)
}
