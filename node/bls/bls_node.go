package node

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/corestario/dkglib/lib/basic"

	bShare "github.com/corestario/dkglib/lib/blsShare"
	dkgOffChain "github.com/corestario/dkglib/lib/offChain"
	dkgtypes "github.com/corestario/dkglib/lib/types"
	"github.com/pkg/errors"
	abci "github.com/tendermint/tendermint/abci/types"
	bcv0 "github.com/tendermint/tendermint/blockchain/v0"
	bcv1 "github.com/tendermint/tendermint/blockchain/v1"
	cfg "github.com/tendermint/tendermint/config"
	cs "github.com/tendermint/tendermint/consensus"
	"github.com/tendermint/tendermint/evidence"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	mempl "github.com/tendermint/tendermint/mempool"
	nd "github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/version"
)

type BLSNodeProvider func(*cfg.Config, log.Logger) (*nd.Node, error)

func createBLSBlockchainReactor(config *cfg.Config,
	state sm.State,
	blockExec *sm.BlockExecutor,
	blockStore *store.BlockStore,
	verifier dkgtypes.Verifier,
	fastSync bool,
	logger log.Logger) (bcReactor p2p.Reactor, err error) {

	switch config.FastSync.Version {
	case "v0":
		bcReactor = bcv0.NewBlockchainReactor(state.Copy(), blockExec, blockStore, fastSync, bcv0.WithVerifier(verifier))
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
	genDoc *types.GenesisDoc) (*cs.ConsensusReactor, *cs.ConsensusState) {
	// Make ConsensusReactor
	evsw := events.NewEventSwitch()

	logger := log.NewTMLogger(os.Stdout)
	dkg, err := basic.NewDKGBasic(
		evsw,
		nd.Cdc,
		state.ChainID,
		config.DKGOnChainConfig.NodeEndpointForContext,
		config.DKGOnChainConfig.Passphrase,
		config.DKGOnChainConfig.RandappCLIDirectory,
		dkgOffChain.WithVerifier(verifier),
		dkgOffChain.WithDKGNumBlocks(genDoc.DKGNumBlocks),
		dkgOffChain.WithLogger(logger),
		dkgOffChain.WithPVKey(privValidator),
	)
	if err != nil {
		panic(err)
	}

	consensusState := cs.NewConsensusState(
		config.Consensus,
		state.Copy(),
		blockExec,
		blockStore,
		mempool,
		evidencePool,
		cs.StateMetrics(csMetrics),
		cs.WithEVSW(evsw),
		cs.WithDKG(dkg),
	)

	consensusState.SetLogger(consensusLogger)
	if privValidator != nil {
		consensusState.SetPrivValidator(privValidator)
	}
	consensusReactor := cs.NewConsensusReactor(consensusState, fastSync, cs.ReactorMetrics(csMetrics))
	consensusReactor.SetLogger(consensusLogger)
	// services which will be publishing and/or subscribing for messages (events)
	// consensusReactor will set it on consensusState and blockExecutor
	consensusReactor.SetEventBus(eventBus)
	return consensusReactor, consensusState
}

func NewBLSNodeForCosmos(config *cfg.Config, logger log.Logger, app abci.Application) (*nd.Node, error) {
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
	options ...nd.Option) (*nd.Node, error) {

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

	var verifier *bShare.BLSVerifier
	fmt.Println("load bls from", config.BLSKeyFile())
	blsShare, err := bShare.LoadBLSShareJSON(config.BLSKeyFile())
	if err == nil {
		keypair, err := blsShare.Deserialize()
		if err != nil {
			return nil, fmt.Errorf("failed to load keypair: %v", err)
		}

		masterPubKey, err := bShare.LoadPubKey(genDoc.BLSMasterPubKey, genDoc.BLSNumShares)
		if err != nil {
			return nil, fmt.Errorf("failed to load master public key from genesis: %v", err)
		}

		verifier = bShare.NewBLSVerifier(masterPubKey, keypair, genDoc.BLSThreshold, genDoc.BLSNumShares)
	} else {
		logger.Info("Failed to load BLS key from", config.BLSKeyFile())
	}

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

	node := &nd.Node{
		Config:        config,
		GenesisDoc:    genDoc,
		PrivValidator: privValidator,

		Transport: transport,
		Sw:        sw,
		AddrBook:  addrBook,
		NodeInfo:  nodeInfo,
		NodeKey:   nodeKey,

		StateDB:          stateDB,
		BlockStore:       blockStore,
		BcReactor:        bcReactor,
		MempoolReactor:   mempoolReactor,
		Mempool:          mempool,
		ConsensusState:   consensusState,
		PexReactor:       pexReactor,
		EvidencePool:     evidencePool,
		ProxyApp:         proxyApp,
		TxIndexer:        txIndexer,
		IndexerService:   indexerService,
		EventBus:         eventBus,
		ConsensusReactor: consensusReactor,
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
	consensusReactor *cs.ConsensusReactor,
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
