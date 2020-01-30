package node

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/corestario/dkglib/lib/basic"

	bShare "github.com/corestario/dkglib/lib/blsShare"
	dkgOffChain "github.com/corestario/dkglib/lib/offChain"
	dkgtypes "github.com/corestario/dkglib/lib/types"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"github.com/tendermint/go-amino"
	abci "github.com/tendermint/tendermint/abci/types"
	bcv0 "github.com/tendermint/tendermint/blockchain/v0"
	bcv1 "github.com/tendermint/tendermint/blockchain/v1"
	cfg "github.com/tendermint/tendermint/config"
	cs "github.com/tendermint/tendermint/consensus"
	"github.com/tendermint/tendermint/evidence"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	tmpubsub "github.com/tendermint/tendermint/libs/pubsub"
	mempl "github.com/tendermint/tendermint/mempool"
	nd "github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	rpccore "github.com/tendermint/tendermint/rpc/core"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	grpccore "github.com/tendermint/tendermint/rpc/grpc"
	rpcserver "github.com/tendermint/tendermint/rpc/lib/server"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/state/txindex"
	"github.com/tendermint/tendermint/store"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
	"github.com/tendermint/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

type BLSNode struct {
	cmn.BaseService

	// config
	config        *cfg.Config
	genesisDoc    *types.GenesisDoc   // initial validator set
	privValidator types.PrivValidator // local node's validator key

	// network
	transport   *p2p.MultiplexTransport
	sw          *p2p.Switch  // p2p connections
	addrBook    pex.AddrBook // known peers
	nodeInfo    p2p.NodeInfo
	nodeKey     *p2p.NodeKey // our node privkey
	isListening bool

	// services
	eventBus         *types.EventBus // pub/sub for services
	stateDB          dbm.DB
	blockStore       *store.BlockStore // store the blockchain to disk
	bcReactor        p2p.Reactor       // for fast-syncing
	mempoolReactor   *mempl.Reactor    // for gossipping transactions
	mempool          mempl.Mempool
	consensusState   cs.StateInterface       // latest consensus state
	consensusReactor *cs.BLSConsensusReactor // for participating in the consensus
	pexReactor       *pex.PEXReactor         // for exchanging peer addresses
	evidencePool     *evidence.EvidencePool  // tracking evidence
	proxyApp         proxy.AppConns          // connection to the application
	rpcListeners     []net.Listener          // rpc servers
	txIndexer        txindex.TxIndexer
	indexerService   *txindex.IndexerService
	prometheusSrv    *http.Server
}

type BLSNodeProvider func(*cfg.Config, log.Logger) (*BLSNode, error)

func createBLSBlockchainReactor(config *cfg.Config,
	state sm.State,
	blockExec *sm.BlockExecutor,
	blockStore *store.BlockStore,
	verifier dkgtypes.Verifier,
	fastSync bool,
	logger log.Logger) (bcReactor p2p.Reactor, err error) {

	switch config.FastSync.Version {
	case "v0":
		bcReactor = bcv0.NewBLSBlockchainReactor(state.Copy(), blockExec, blockStore, verifier, fastSync)
	case "v1":
		bcReactor = bcv1.NewBlockchainReactor(state.Copy(), blockExec, blockStore, fastSync)
	default:
		return nil, fmt.Errorf("unknown fastsync version %s", config.FastSync.Version)
	}

	bcReactor.SetLogger(logger.With("module", "blockchain"))
	return bcReactor, nil
}

func createBLSConsensus(config *cfg.Config,
	state sm.State,
	blockExec *sm.BlockExecutor,
	blockStore sm.BlockStore,
	mempool *mempl.CListMempool,
	evidencePool *evidence.EvidencePool,
	privValidator types.PrivValidator,
	csMetrics *cs.Metrics,
	fastSync bool,
	eventBus *types.EventBus,
	consensusLogger log.Logger,
	verifier dkgtypes.Verifier,
	genDoc *types.GenesisDoc) (*cs.BLSConsensusReactor, cs.StateInterface) {
	// Make ConsensusReactor
	evsw := events.NewEventSwitch()

	logger := log.NewTMLogger(os.Stdout)
	dkg, err := basic.NewDKGBasic(
		evsw,
		nd.Cdc,
		state.ChainID,
		config.NodeEndpointForContext,
		config.RandappCLIDirectory,
		dkgOffChain.WithVerifier(verifier),
		dkgOffChain.WithDKGNumBlocks(genDoc.DKGNumBlocks),
		dkgOffChain.WithLogger(logger),
		dkgOffChain.WithPVKey(privValidator),
	)
	if err != nil {
		panic(err)
	}

	consensusState := cs.NewBLSConsensusState(
		config.Consensus,
		state.Copy(),
		blockExec,
		blockStore,
		mempool,
		evidencePool,
		cs.BLSStateMetrics(csMetrics),
		cs.BLSWithEVSW(evsw),
		cs.BLSWithDKG(dkg),
	)

	consensusState.SetLogger(consensusLogger)
	if privValidator != nil {
		consensusState.SetPrivValidator(privValidator)
	}
	consensusReactor := cs.NewBLSConsensusReactor(consensusState, fastSync, cs.BLSReactorMetrics(csMetrics))
	consensusReactor.SetLogger(consensusLogger)
	// services which will be publishing and/or subscribing for messages (events)
	// consensusReactor will set it on consensusState and blockExecutor
	consensusReactor.SetEventBus(eventBus)
	return consensusReactor, consensusState
}

type BLSOption func(*BLSNode)

func NewBLSNodeForCosmos(config *cfg.Config, logger log.Logger, app abci.Application) (*BLSNode, error) {
	nodeKey, err := p2p.LoadOrGenNodeKey(config.NodeKeyFile())
	if err != nil {

		return nil, err

	}

	// Convert old PrivValidator if it exists.
	oldPrivVal := config.OldPrivValidatorFile()
	newPrivValKey := config.PrivValidatorKeyFile()
	newPrivValState := config.PrivValidatorStateFile()
	if _, err := os.Stat(oldPrivVal); !os.IsNotExist(err) {
		oldPV, err := privval.LoadOldFilePV(oldPrivVal)
		if err != nil {

			return nil, fmt.Errorf("error reading OldPrivValidator from %v: %v\n", oldPrivVal, err)

		}
		logger.Info("Upgrading PrivValidator file",
			"old", oldPrivVal,
			"newKey", newPrivValKey,
			"newState", newPrivValState,
		)

		oldPV.Upgrade(newPrivValKey, newPrivValState)
	}

	return NewBLSNode(
		config,
		privval.LoadOrGenFilePV(newPrivValKey, newPrivValState),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		nd.DefaultGenesisDocProviderFunc(config),
		nd.DefaultDBProvider,
		nd.DefaultMetricsProvider(config.Instrumentation),
		logger,
	)
}

// NewNode returns a new, ready to go, Tendermint Node.
func NewBLSNode(config *cfg.Config,
	privValidator types.PrivValidator,
	nodeKey *p2p.NodeKey,
	clientCreator proxy.ClientCreator,
	genesisDocProvider nd.GenesisDocProvider,
	dbProvider nd.DBProvider,
	metricsProvider nd.MetricsProvider,
	logger log.Logger,
	//blockStore *store.BlockStore,
	//stateDB dbm.DB,
	options ...BLSOption) (*BLSNode, error) {

	blockStore, stateDB, err := nd.InitDBs(config, nd.DefaultDBProvider)
	if err != nil {
		return nil, err
	}

	state, genDoc, err := nd.LoadStateFromDBOrGenesisDocProvider(stateDB, genesisDocProvider)
	if err != nil {
		return nil, err
	}

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := nd.CreateAndStartProxyAppConns(clientCreator, logger)
	if err != nil {
		return nil, err
	}

	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped after it saved the block
	// but before it indexed the txs, or, endblocker panicked)
	eventBus, err := nd.CreateAndStartEventBus(logger)
	if err != nil {
		return nil, err
	}

	// Transaction indexing
	indexerService, txIndexer, err := nd.CreateAndStartIndexerService(config, dbProvider, eventBus, logger)
	if err != nil {
		return nil, err
	}

	// Create the handshaker, which calls RequestInfo, sets the AppVersion on the state,
	// and replays any blocks as necessary to sync tendermint with the app.
	consensusLogger := logger.With("module", "consensus")
	if err := nd.DoHandshake(stateDB, state, blockStore, genDoc, eventBus, proxyApp, consensusLogger); err != nil {
		return nil, err
	}

	// Reload the state. It will have the Version.Consensus.App set by the
	// Handshake, and may have other modifications as well (ie. depending on
	// what happened during block replay).
	state = sm.LoadState(stateDB)

	// If an address is provided, listen on the socket for a connection from an
	// external signing process.
	if config.PrivValidatorListenAddr != "" {
		// FIXME: we should start services inside OnStart
		privValidator, err = nd.CreateAndStartPrivValidatorSocketClient(config.PrivValidatorListenAddr, logger)
		if err != nil {
			return nil, errors.Wrap(err, "error with private validator socket client")
		}
	}

	logBLSNodeStartupInfo(state, privValidator, logger, consensusLogger)

	// Decide whether to fast-sync or not
	// We don't fast-sync when the only validator is us.
	fastSync := config.FastSyncMode && !nd.OnlyValidatorIsUs(state, privValidator)

	csMetrics, p2pMetrics, memplMetrics, smMetrics := metricsProvider(genDoc.ChainID)

	// Make MempoolReactor
	mempoolReactor, mempool := nd.CreateMempoolAndMempoolReactor(config, proxyApp, state, memplMetrics, logger)

	// Make Evidence Reactor
	evidenceReactor, evidencePool, err := nd.CreateEvidenceReactor(config, dbProvider, stateDB, logger)
	if err != nil {
		return nil, err
	}

	// make block executor for consensus and blockchain reactors to execute blocks
	blockExec := sm.NewBlockExecutor(
		stateDB,
		logger.With("module", "state"),
		proxyApp.Consensus(),
		mempool,
		evidencePool,
		sm.BlockExecutorWithMetrics(smMetrics),
	)

	fmt.Println("load bls from", config.BLSKeyFile())
	blsShare, err := bShare.LoadBLSShareJSON(config.BLSKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load BLS keypair: %v", err)
	}

	keypair, err := blsShare.Deserialize()
	if err != nil {
		return nil, fmt.Errorf("failed to load keypair: %v", err)
	}

	masterPubKey, err := bShare.LoadPubKey(genDoc.BLSMasterPubKey, genDoc.BLSNumShares)
	if err != nil {
		return nil, fmt.Errorf("failed to load master public key from genesis: %v", err)
	}

	verifier := bShare.NewBLSVerifier(masterPubKey, keypair, genDoc.BLSThreshold, genDoc.BLSNumShares)
	// Make BlockchainReactor

	bcReactor, err := createBLSBlockchainReactor(config, state, blockExec, blockStore, verifier, fastSync, logger)
	if err != nil {
		return nil, errors.Wrap(err, "could not create blockchain reactor")
	}

	// Make ConsensusReactor
	consensusReactor, consensusState := createBLSConsensus(
		config, state, blockExec, blockStore, mempool, evidencePool,
		privValidator, csMetrics, fastSync, eventBus, consensusLogger, verifier, genDoc,
	)

	nodeInfo, err := nd.MakeNodeInfo(config, nodeKey, txIndexer, genDoc, state)
	if err != nil {
		return nil, err
	}

	// Setup Transport.
	transport, peerFilters := nd.CreateTransport(config, nodeInfo, nodeKey, proxyApp)
	// Setup Switch.
	p2pLogger := logger.With("module", "p2p")
	sw := createBLSSwitch(
		config, transport, p2pMetrics, peerFilters, mempoolReactor, bcReactor,
		consensusReactor, evidenceReactor, nodeInfo, nodeKey, p2pLogger,
	)
	err = sw.AddPersistentPeers(nd.SplitAndTrimEmpty(config.P2P.PersistentPeers, ",", " "))
	if err != nil {
		return nil, errors.Wrap(err, "could not add peers from persistent_peers field")
	}
	addrBook, err := nd.CreateAddrBookAndSetOnSwitch(config, sw, p2pLogger, nodeKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not create addrbook")
	}

	// Optionally, start the pex reactor
	//
	// TODO:
	//
	// We need to set Seeds and PersistentPeers on the switch,
	// since it needs to be able to use these (and their DNS names)
	// even if the PEX is off. We can include the DNS name in the NetAddress,
	// but it would still be nice to have a clear list of the current "PersistentPeers"
	// somewhere that we can return with net_info.
	//
	// If PEX is on, it should handle dialing the seeds. Otherwise the switch does it.
	// Note we currently use the addrBook regardless at least for AddOurAddress
	var pexReactor *pex.PEXReactor
	if config.P2P.PexReactor {
		pexReactor = nd.CreatePEXReactorAndAddToSwitch(addrBook, config, sw, logger)
	}
	if config.ProfListenAddress != "" {
		go func() {
			logger.Error("Profile server", "err", http.ListenAndServe(config.ProfListenAddress, nil))
		}()
	}

	node := &BLSNode{
		config:        config,
		genesisDoc:    genDoc,
		privValidator: privValidator,

		transport: transport,
		sw:        sw,
		addrBook:  addrBook,
		nodeInfo:  nodeInfo,
		nodeKey:   nodeKey,

		stateDB:          stateDB,
		blockStore:       blockStore,
		bcReactor:        bcReactor,
		mempoolReactor:   mempoolReactor,
		mempool:          mempool,
		consensusState:   consensusState,
		consensusReactor: consensusReactor,
		pexReactor:       pexReactor,
		evidencePool:     evidencePool,
		proxyApp:         proxyApp,
		txIndexer:        txIndexer,
		indexerService:   indexerService,
		eventBus:         eventBus,
	}

	node.BaseService = *cmn.NewBaseService(logger, "Node", node)

	for _, option := range options {
		option(node)
	}

	return node, nil
}

func createBLSSwitch(config *cfg.Config,
	transport p2p.Transport,
	p2pMetrics *p2p.Metrics,
	peerFilters []p2p.PeerFilterFunc,
	mempoolReactor *mempl.Reactor,
	bcReactor p2p.Reactor,
	consensusReactor *cs.BLSConsensusReactor,
	evidenceReactor *evidence.EvidenceReactor,
	nodeInfo p2p.NodeInfo,
	nodeKey *p2p.NodeKey,
	p2pLogger log.Logger) *p2p.Switch {

	sw := p2p.NewSwitch(
		config.P2P,
		transport,
		p2p.WithMetrics(p2pMetrics),
		p2p.SwitchPeerFilters(peerFilters...),
	)
	sw.SetLogger(p2pLogger)
	sw.AddReactor("MEMPOOL", mempoolReactor)
	sw.AddReactor("BLOCKCHAIN", bcReactor)
	sw.AddReactor("CONSENSUS", consensusReactor)
	sw.AddReactor("EVIDENCE", evidenceReactor)

	sw.SetNodeInfo(nodeInfo)
	sw.SetNodeKey(nodeKey)

	p2pLogger.Info("P2P Node ID", "ID", nodeKey.ID(), "file", config.NodeKeyFile())
	return sw
}

func logBLSNodeStartupInfo(state sm.State, privValidator types.PrivValidator, logger,
	consensusLogger log.Logger) {

	// Log the version info.
	logger.Info("Version info",
		"software", version.TMCoreSemVer,
		"block", version.BlockProtocol,
		"p2p", version.P2PProtocol,
	)

	// If the state and software differ in block version, at least log it.
	if state.Version.Consensus.Block != version.BlockProtocol {
		logger.Info("Software and state have different block protocols",
			"software", version.BlockProtocol,
			"state", state.Version.Consensus.Block,
		)
	}

	pubKey := privValidator.GetPubKey()
	addr := pubKey.Address()
	// Log whether this node is a validator or an observer
	if state.Validators.HasAddress(addr) {
		consensusLogger.Info("This node is a validator", "addr", addr, "pubKey", pubKey)
	} else {
		consensusLogger.Info("This node is not a validator", "addr", addr, "pubKey", pubKey)
	}
}

// OnStart starts the Node. It implements cmn.Service.
func (n *BLSNode) OnStart() error {
	now := tmtime.Now()
	genTime := n.genesisDoc.GenesisTime
	if genTime.After(now) {
		n.Logger.Info("Genesis time is in the future. Sleeping until then...", "genTime", genTime)
		time.Sleep(genTime.Sub(now))
	}

	// Add private IDs to addrbook to block those peers being added
	n.addrBook.AddPrivateIDs(nd.SplitAndTrimEmpty(n.config.P2P.PrivatePeerIDs, ",", " "))

	// Start the RPC server before the P2P server
	// so we can eg. receive txs for the first block
	if n.config.RPC.ListenAddress != "" {
		listeners, err := n.startRPC()
		if err != nil {
			return err
		}
		n.rpcListeners = listeners
	}

	if n.config.Instrumentation.Prometheus &&
		n.config.Instrumentation.PrometheusListenAddr != "" {
		n.prometheusSrv = n.startPrometheusServer(n.config.Instrumentation.PrometheusListenAddr)
	}

	// Start the transport.
	addr, err := p2p.NewNetAddressString(p2p.IDAddressString(n.nodeKey.ID(), n.config.P2P.ListenAddress))
	if err != nil {
		return err
	}
	if err := n.transport.Listen(*addr); err != nil {
		return err
	}

	n.isListening = true

	if n.config.Mempool.WalEnabled() {
		n.mempool.InitWAL() // no need to have the mempool wal during tests
	}

	// Start the switch (the P2P server).
	err = n.sw.Start()
	if err != nil {
		return err
	}

	// Always connect to persistent peers
	err = n.sw.DialPeersAsync(nd.SplitAndTrimEmpty(n.config.P2P.PersistentPeers, ",", " "))
	if err != nil {
		return errors.Wrap(err, "could not dial peers from persistent_peers field")
	}

	return nil
}

// OnStop stops the Node. It implements cmn.Service.
func (n *BLSNode) OnStop() {
	n.BaseService.OnStop()

	n.Logger.Info("Stopping Node")

	// first stop the non-reactor services
	n.eventBus.Stop()
	n.indexerService.Stop()

	// now stop the reactors
	n.sw.Stop()

	// stop mempool WAL
	if n.config.Mempool.WalEnabled() {
		n.mempool.CloseWAL()
	}

	if err := n.transport.Close(); err != nil {
		n.Logger.Error("Error closing transport", "err", err)
	}

	n.isListening = false

	// finally stop the listeners / external services
	for _, l := range n.rpcListeners {
		n.Logger.Info("Closing rpc listener", "listener", l)
		if err := l.Close(); err != nil {
			n.Logger.Error("Error closing listener", "listener", l, "err", err)
		}
	}

	if pvsc, ok := n.privValidator.(cmn.Service); ok {
		pvsc.Stop()
	}

	if n.prometheusSrv != nil {
		if err := n.prometheusSrv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			n.Logger.Error("Prometheus HTTP server Shutdown", "err", err)
		}
	}
}

// ConfigureRPC sets all variables in rpccore so they will serve
// rpc calls from this node
func (n *BLSNode) ConfigureRPC() {
	rpccore.SetStateDB(n.stateDB)
	rpccore.SetBlockStore(n.blockStore)
	rpccore.SetConsensusState(n.consensusState)
	rpccore.SetMempool(n.mempool)
	rpccore.SetEvidencePool(n.evidencePool)
	rpccore.SetP2PPeers(n.sw)
	rpccore.SetP2PTransport(n)
	pubKey := n.privValidator.GetPubKey()
	rpccore.SetPubKey(pubKey)
	rpccore.SetGenesisDoc(n.genesisDoc)
	rpccore.SetProxyAppQuery(n.proxyApp.Query())
	rpccore.SetTxIndexer(n.txIndexer)
	rpccore.SetConsensusReactor(n.consensusReactor)
	rpccore.SetEventBus(n.eventBus)
	rpccore.SetLogger(n.Logger.With("module", "rpc"))
	rpccore.SetConfig(*n.config.RPC)
}

func (n *BLSNode) startRPC() ([]net.Listener, error) {
	n.ConfigureRPC()
	listenAddrs := nd.SplitAndTrimEmpty(n.config.RPC.ListenAddress, ",", " ")
	coreCodec := amino.NewCodec()
	ctypes.RegisterAmino(coreCodec)

	if n.config.RPC.Unsafe {
		rpccore.AddUnsafeRoutes()
	}

	config := rpcserver.DefaultConfig()
	config.MaxBodyBytes = n.config.RPC.MaxBodyBytes
	config.MaxHeaderBytes = n.config.RPC.MaxHeaderBytes
	config.MaxOpenConnections = n.config.RPC.MaxOpenConnections
	// If necessary adjust global WriteTimeout to ensure it's greater than
	// TimeoutBroadcastTxCommit.
	// See https://github.com/tendermint/tendermint/issues/3435
	if config.WriteTimeout <= n.config.RPC.TimeoutBroadcastTxCommit {
		config.WriteTimeout = n.config.RPC.TimeoutBroadcastTxCommit + 1*time.Second
	}

	// we may expose the rpc over both a unix and tcp socket
	listeners := make([]net.Listener, len(listenAddrs))
	for i, listenAddr := range listenAddrs {
		mux := http.NewServeMux()
		rpcLogger := n.Logger.With("module", "rpc-server")
		wmLogger := rpcLogger.With("protocol", "websocket")
		wm := rpcserver.NewWebsocketManager(rpccore.Routes, coreCodec,
			rpcserver.OnDisconnect(func(remoteAddr string) {
				err := n.eventBus.UnsubscribeAll(context.Background(), remoteAddr)
				if err != nil && err != tmpubsub.ErrSubscriptionNotFound {
					wmLogger.Error("Failed to unsubscribe addr from events", "addr", remoteAddr, "err", err)
				}
			}),
			rpcserver.ReadLimit(config.MaxBodyBytes),
		)
		wm.SetLogger(wmLogger)
		mux.HandleFunc("/websocket", wm.WebsocketHandler)
		rpcserver.RegisterRPCFuncs(mux, rpccore.Routes, coreCodec, rpcLogger)
		listener, err := rpcserver.Listen(
			listenAddr,
			config,
		)
		if err != nil {
			return nil, err
		}

		var rootHandler http.Handler = mux
		if n.config.RPC.IsCorsEnabled() {
			corsMiddleware := cors.New(cors.Options{
				AllowedOrigins: n.config.RPC.CORSAllowedOrigins,
				AllowedMethods: n.config.RPC.CORSAllowedMethods,
				AllowedHeaders: n.config.RPC.CORSAllowedHeaders,
			})
			rootHandler = corsMiddleware.Handler(mux)
		}
		if n.config.RPC.IsTLSEnabled() {
			go rpcserver.StartHTTPAndTLSServer(
				listener,
				rootHandler,
				n.config.RPC.CertFile(),
				n.config.RPC.KeyFile(),
				rpcLogger,
				config,
			)
		} else {
			go rpcserver.StartHTTPServer(
				listener,
				rootHandler,
				rpcLogger,
				config,
			)
		}

		listeners[i] = listener
	}

	// we expose a simplified api over grpc for convenience to app devs
	grpcListenAddr := n.config.RPC.GRPCListenAddress
	if grpcListenAddr != "" {
		config := rpcserver.DefaultConfig()
		config.MaxOpenConnections = n.config.RPC.MaxOpenConnections
		listener, err := rpcserver.Listen(grpcListenAddr, config)
		if err != nil {
			return nil, err
		}
		go grpccore.StartGRPCServer(listener)
		listeners = append(listeners, listener)
	}

	return listeners, nil
}

// startPrometheusServer starts a Prometheus HTTP server, listening for metrics
// collectors on addr.
func (n *BLSNode) startPrometheusServer(addr string) *http.Server {
	srv := &http.Server{
		Addr: addr,
		Handler: promhttp.InstrumentMetricHandler(
			prometheus.DefaultRegisterer, promhttp.HandlerFor(
				prometheus.DefaultGatherer,
				promhttp.HandlerOpts{MaxRequestsInFlight: n.config.Instrumentation.MaxOpenConnections},
			),
		),
	}
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			// Error starting or closing listener:
			n.Logger.Error("Prometheus HTTP server ListenAndServe", "err", err)
		}
	}()
	return srv
}

// Switch returns the Node's Switch.
func (n *BLSNode) Switch() *p2p.Switch {
	return n.sw
}

// BlockStore returns the Node's BlockStore.
func (n *BLSNode) BlockStore() *store.BlockStore {
	return n.blockStore
}

// ConsensusState returns the Node's ConsensusState.
func (n *BLSNode) ConsensusState() cs.StateInterface {
	return n.consensusState
}

// ConsensusReactor returns the Node's ConsensusReactor.
func (n *BLSNode) ConsensusReactor() *cs.BLSConsensusReactor {
	return n.consensusReactor
}

// MempoolReactor returns the Node's mempool reactor.
func (n *BLSNode) MempoolReactor() *mempl.Reactor {
	return n.mempoolReactor
}

// Mempool returns the Node's mempool.
func (n *BLSNode) Mempool() mempl.Mempool {
	return n.mempool
}

// PEXReactor returns the Node's PEXReactor. It returns nil if PEX is disabled.
func (n *BLSNode) PEXReactor() *pex.PEXReactor {
	return n.pexReactor
}

// EvidencePool returns the Node's EvidencePool.
func (n *BLSNode) EvidencePool() *evidence.EvidencePool {
	return n.evidencePool
}

// EventBus returns the Node's EventBus.
func (n *BLSNode) EventBus() *types.EventBus {
	return n.eventBus
}

// PrivValidator returns the Node's PrivValidator.
// XXX: for convenience only!
func (n *BLSNode) PrivValidator() types.PrivValidator {
	return n.privValidator
}

// GenesisDoc returns the Node's GenesisDoc.
func (n *BLSNode) GenesisDoc() *types.GenesisDoc {
	return n.genesisDoc
}

// ProxyApp returns the Node's AppConns, representing its connections to the ABCI application.
func (n *BLSNode) ProxyApp() proxy.AppConns {
	return n.proxyApp
}

// Config returns the Node's config.
func (n *BLSNode) Config() *cfg.Config {
	return n.config
}

//------------------------------------------------------------------------------

func (n *BLSNode) Listeners() []string {
	return []string{
		fmt.Sprintf("Listener(@%v)", n.config.P2P.ExternalAddress),
	}
}

func (n *BLSNode) IsListening() bool {
	return n.isListening
}

// NodeInfo returns the Node's Info from the Switch.
func (n *BLSNode) NodeInfo() p2p.NodeInfo {
	return n.nodeInfo
}
