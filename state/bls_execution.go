package state

import (
	dkgTypes "github.com/dgamingfoundation/dkglib/lib/types"
	"github.com/tendermint/tendermint/libs/log"
	mempl "github.com/tendermint/tendermint/mempool"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

type BLSBlockExecutor struct {
	*BlockExecutor
	dkgInstance dkgTypes.DKG
}

func NewBLSBlockExecutor(db dbm.DB, logger log.Logger, proxyApp proxy.AppConnConsensus, mempool mempl.Mempool, evpool EvidencePool, options ...BlockExecutorOption) *BLSBlockExecutor {
	res := &BlockExecutor{
		db:       db,
		proxyApp: proxyApp,
		eventBus: types.NopEventBus{},
		mempool:  mempool,
		evpool:   evpool,
		logger:   logger,
		metrics:  NopMetrics(),
	}

	for _, option := range options {
		option(res)
	}

	return &BLSBlockExecutor{
		BlockExecutor: res,
	}
}

func (blockExec *BLSBlockExecutor) SetDKGInstance(dkgInstance dkgTypes.DKG) {
	blockExec.dkgInstance = dkgInstance
}

func (blockExec *BLSBlockExecutor) ValidateBlock(state State, block *types.Block) error {
	return blsValidateBlock(blockExec.evpool, blockExec.db, state, blockExec.dkgInstance, block)
}
