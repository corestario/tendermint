package node

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"

	"github.com/dgamingfoundation/dkglib/lib/basic"
	bShare "github.com/dgamingfoundation/dkglib/lib/blsShare"
	dkgOffChain "github.com/dgamingfoundation/dkglib/lib/offChain"
	dkgtypes "github.com/dgamingfoundation/dkglib/lib/types"
	"github.com/pkg/errors"
	bcv0 "github.com/tendermint/tendermint/blockchain/v0"
	bcv1 "github.com/tendermint/tendermint/blockchain/v1"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/consensus"
	cs "github.com/tendermint/tendermint/consensus"
	"github.com/tendermint/tendermint/evidence"
	cmn "github.com/tendermint/tendermint/libs/service"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	mempl "github.com/tendermint/tendermint/mempool"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/proxy"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

func GetBLSReactors(
	config *cfg.Config,
	privValidator types.PrivValidator,
	metricsProvider MetricsProvider,
	logger log.Logger,
) (p2p.Reactor, *consensus.Reactor, consensus.StateInterface, error) {
	consensusLogger := logger.With("module", "consensus")

	blockStore, stateDB, err := initDBs(config, DefaultDBProvider)
	if err != nil {
		return nil, nil, nil, err
	}

	state, genDoc, err := LoadStateFromDBOrGenesisDocProvider(stateDB, DefaultGenesisDocProviderFunc(config))
	if err != nil {
		return nil, nil, nil, err
	}

	fastSync := config.FastSyncMode && !onlyValidatorIsUs(state, privValidator)

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := createAndStartProxyAppConns(proxy.DefaultClientCreator(config.ProxyApp, config.ABCI, config.DBDir()), logger)
	if err != nil {
		return nil, nil, nil, err
	}

	csMetrics, _, memplMetrics, smMetrics := metricsProvider(genDoc.ChainID)

	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped after it saved the block
	// but before it indexed the txs, or, endblocker panicked)
	eventBus, err := createAndStartEventBus(logger)
	if err != nil {
		return nil, nil, nil, err
	}

	// Make MempoolReactor
	_, mempool := createMempoolAndMempoolReactor(config, proxyApp, state, memplMetrics, logger)

	// Make Evidence Reactor
	_, evidencePool, err := createEvidenceReactor(config, DefaultDBProvider, stateDB, logger)
	if err != nil {
		return nil, nil, nil, err
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

	blsShare, err := bShare.LoadBLSShareJSON(config.BLSKeyFile())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load bls share: %v", err)
	}

	keypair, err := blsShare.Deserialize()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to deserialize keypair: %v", err)
	}

	masterPubKey, err := bShare.LoadPubKey(genDoc.BLSMasterPubKey, genDoc.BLSNumShares)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load master public key from genesis: %v", err)
	}

	verifier := bShare.NewBLSVerifier(masterPubKey, keypair, genDoc.BLSThreshold, genDoc.BLSNumShares)

	// Make BlockchainReactor
	bcReactor, err := createBLSBlockchainReactor(config, state, blockExec, blockStore, verifier, fastSync, consensusLogger)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "could not create blockchain reactor")
	}

	// Make ConsensusReactor
	consensusReactor, consensusState := createBLSConsensus(
		config, state, blockExec, blockStore, mempool, evidencePool,
		privValidator, csMetrics, fastSync, eventBus, consensusLogger, verifier, genDoc.DKGNumBlocks,
	)

	config.DBPath = config.DBPath + ".unused"

	return bcReactor, consensusReactor, consensusState, nil
}

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
	dkgNumBlocks int64) (*consensus.Reactor, consensus.StateInterface) {
	// Make ConsensusReactor
	evsw := events.NewEventSwitch()

	dkg, err := basic.NewDKGBasic(
		evsw,
		cdc,
		"tendermintChain",
		"tcp://localhost:26657",
		config.RootDir,
		dkgOffChain.WithVerifier(verifier),
		dkgOffChain.WithDKGNumBlocks(dkgNumBlocks),
		dkgOffChain.WithLogger(consensusLogger.With("dkg")),
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
	consensusReactor := cs.NewReactor(&consensusState.ConsensusState, fastSync, cs.ReactorMetrics(csMetrics))
	consensusReactor.SetLogger(consensusLogger)
	// services which will be publishing and/or subscribing for messages (events)
	// consensusReactor will set it on consensusState and blockExecutor
	consensusReactor.SetEventBus(eventBus)
	return consensusReactor, consensusState
}

// NewNode returns a new, ready to go, Tendermint Node.
func NewBLSNode(config *cfg.Config,
	privValidator types.PrivValidator,
	nodeKey *p2p.NodeKey,
	clientCreator proxy.ClientCreator,
	genesisDocProvider GenesisDocProvider,
	dbProvider DBProvider,
	metricsProvider MetricsProvider,
	logger log.Logger,
	options ...Option) (*Node, error) {

	blockStore, stateDB, err := initDBs(config, dbProvider)
	if err != nil {
		return nil, err
	}

	state, genDoc, err := LoadStateFromDBOrGenesisDocProvider(stateDB, genesisDocProvider)
	if err != nil {
		return nil, err
	}

	// Create the proxyApp and establish connections to the ABCI app (consensus, mempool, query).
	proxyApp, err := createAndStartProxyAppConns(clientCreator, logger)
	if err != nil {
		return nil, err
	}

	// EventBus and IndexerService must be started before the handshake because
	// we might need to index the txs of the replayed block as this might not have happened
	// when the node stopped last time (i.e. the node stopped after it saved the block
	// but before it indexed the txs, or, endblocker panicked)
	eventBus, err := createAndStartEventBus(logger)
	if err != nil {
		return nil, err
	}

	// Transaction indexing
	indexerService, txIndexer, err := createAndStartIndexerService(config, dbProvider, eventBus, logger)
	if err != nil {
		return nil, err
	}

	// Create the handshaker, which calls RequestInfo, sets the AppVersion on the state,
	// and replays any blocks as necessary to sync tendermint with the app.
	consensusLogger := logger.With("module", "consensus")
	if err := doHandshake(stateDB, state, blockStore, genDoc, eventBus, proxyApp, consensusLogger); err != nil {
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
		privValidator, err = createAndStartPrivValidatorSocketClient(config.PrivValidatorListenAddr, logger)
		if err != nil {
			return nil, errors.Wrap(err, "error with private validator socket client")
		}
	}

	logBLSNodeStartupInfo(state, privValidator, logger, consensusLogger)

	// Decide whether to fast-sync or not
	// We don't fast-sync when the only validator is us.
	fastSync := config.FastSyncMode && !onlyValidatorIsUs(state, privValidator)

	csMetrics, p2pMetrics, memplMetrics, smMetrics := metricsProvider(genDoc.ChainID)

	// Make MempoolReactor
	mempoolReactor, mempool := createMempoolAndMempoolReactor(config, proxyApp, state, memplMetrics, logger)

	// Make Evidence Reactor
	evidenceReactor, evidencePool, err := createEvidenceReactor(config, dbProvider, stateDB, logger)
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
		privValidator, csMetrics, fastSync, eventBus, consensusLogger, verifier, genDoc.DKGNumBlocks,
	)

	nodeInfo, err := makeNodeInfo(config, nodeKey, txIndexer, genDoc, state)
	if err != nil {
		return nil, err
	}

	// Setup Transport.
	transport, peerFilters := createTransport(config, nodeInfo, nodeKey, proxyApp)

	// Setup Switch.
	p2pLogger := logger.With("module", "p2p")
	sw := createSwitch(
		config, transport, p2pMetrics, peerFilters, mempoolReactor, bcReactor,
		consensusReactor, evidenceReactor, nodeInfo, nodeKey, p2pLogger,
	)

	err = sw.AddPersistentPeers(splitAndTrimEmpty(config.P2P.PersistentPeers, ",", " "))
	if err != nil {
		return nil, errors.Wrap(err, "could not add peers from persistent_peers field")
	}

	addrBook, err := createAddrBookAndSetOnSwitch(config, sw, p2pLogger, nodeKey)
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
	var pexReactor *pex.Reactor
	if config.P2P.PexReactor {
		pexReactor = createPEXReactorAndAddToSwitch(addrBook, config, sw, logger)
	}

	if config.ProfListenAddress != "" {
		go func() {
			logger.Error("Profile server", "err", http.ListenAndServe(config.ProfListenAddress, nil))
		}()
	}

	node := &Node{
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
