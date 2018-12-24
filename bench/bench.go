package main

import (
	"flag"
	"fmt"
	"github.com/json-iterator/go"
	"io/ioutil"
	"math/big"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

	rps       = flag.Int("rps", 500, "syncTx|asyncTx|commitTx")
	blockTime = flag.Int("blocktime", 6, "block time in seconds")
	txSize    = flag.Int("txsize", 100, "transaction size in bytes")
	duration  = flag.Duration("duration", 10*time.Minute, "test duration in format: [0-9]*[ms,s,m,h,d,y]")
	threads   = flag.Int("threads", 20*runtime.NumCPU(), "how many threads to run")
	writeTxs  = flag.Int("writetxs", 100, "a fraction of write transactions. a non-negative number between o and 100")

	expectedBlockSize int
	txTime            time.Duration
	unconfirmedTxsNum string
	postTxURL         string
	getTxURL          string
)

func main() {
	initParams()

	deadlineTimer := time.NewTimer(*duration)
	defer deadlineTimer.Stop()

	logFreq := (*rps) * 10
	if logFreq > 10000 {
		logFreq = 10000
	}

	fmt.Println("going to fire at rps rate", *rps)
	fmt.Println("tx size in bytes", *txSize)
	fmt.Println("a block size is expected to be", expectedBlockSize)

	var totalTime time.Duration
	startTime := time.Now()

	senders := newSenderGroup(*threads, startTime, *rps, logFreq, currentTx, int32(*writeTxs))
	senders.run()

mainLoop:
	for {
		select {
		case <-deadlineTimer.C:
			senders.stop()
			break mainLoop
		default:
			//nothing
		}
	}

	// wait until all txs passed
	waitUnconfirmedTxs()

	fmt.Println("Done", atomic.LoadUint64(currentTx))
	fmt.Println("Total time", totalTime)
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
}

func newSenderGroup(n int, startTime time.Time, estimatedRPS int, logFreq int, currentTx *uint64, writeTxProbability int32) senderGroup {
	senders := make([]*sender, n)
	storage := new(hashStorage)

	for i := 0; i < n; i++ {
		senders[i] = newSender(startTime, estimatedRPS, logFreq, currentTx, writeTxProbability, storage)
	}

	return senderGroup{senders: senders}
}

func (group senderGroup) run() {
	n := len(group.senders)
	cancels := make([]chan struct{}, n)

	for i := 0; i < n; i++ {
		cancels[i] = group.senders[i].run()
	}
}

func (group senderGroup) stop() {
	n := len(group.senders)

	for i := 0; i < n; i++ {
		close(group.cancels[i])
	}
}

type sender struct {
	startTime          time.Time
	estimatedRPS       int
	logFreq            int
	currentTx          *uint64
	writeTxProbability int32
	storage            *hashStorage

	cancel chan struct{}
}

func newSender(startTime time.Time, estimatedRPS int, logFreq int, currentTx *uint64, writeTxProbability int32, storage *hashStorage) *sender {
	return &sender{
		startTime:          startTime,
		estimatedRPS:       estimatedRPS,
		logFreq:            logFreq,
		currentTx:          currentTx,
		writeTxProbability: writeTxProbability,
		storage:            storage,
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
			time.Sleep(10 * time.Millisecond)

			continue
		}

		if s.isPostTxTurn(txIndex) {
			hash := postTx(txIndex)
			s.storage.storeTx(hash)
		} else {
			getTxByHash(s.storage.getAnyTxHash())
		}

		log(s.startTime, txIndex, uint64(s.logFreq))

		break
	}
}

func (s *sender) isPostTxTurn(txIndex uint64) bool {
	if !s.storage.hasAny() {
		return true
	}

	return rand.Int31n(100) < s.writeTxProbability
}

func postTx(n uint64) string {
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
	resRaw := doRequest(postTxURL + "\"" + txPrefix + tx + "\"")
	decode(resRaw, res)

	return res.Res.Hash
}

func getTxByHash(hash string) {
	doRequest(getTxURL + hash)
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

func log(mainTime time.Time, i uint64, logFreq uint64) time.Duration {
	endTime := time.Now()
	roundTime := endTime.Sub(mainTime)

	freq := big.NewRat(int64(i+1), int64(roundTime))
	rps, _ := freq.Mul(freq, big.NewRat(int64(time.Second), 1)).Float64()

	if i%logFreq != 0 {
		return roundTime
	}

	fmt.Printf("Total time till Tx %d: %v. Total test duration %v. rps: %v\n",
		i, roundTime, roundTime, rps)

	hasUnconfirmedTxs(true)

	return roundTime
}

func hasUnconfirmedTxs(withLog bool) bool {
	res := doRequest(unconfirmedTxsNum)

	resp := new(UnconfirmedTxsResponse)
	decode(res, resp)

	if withLog {
		fmt.Println("Has Unconfirmed Txs", string(res))
	}

	n, err := strconv.Atoi(resp.Res.N)
	if err != nil {
		fmt.Printf("error while getting unconfirmed TXs: %v, %q\n", err, string(res))
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

func doRequest(url string) []byte {
	resp, err := httpClient.Get(url)
	if err != nil {
		fmt.Println("error while http.get", err)
		return nil
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("error while reading response body", err)
		return nil
	}

	return body
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
