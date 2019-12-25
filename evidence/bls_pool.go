package evidence

import (
	dkgTypes "github.com/dgamingfoundation/dkglib/lib/types"
	clist "github.com/tendermint/tendermint/libs/clist"
	"github.com/tendermint/tendermint/libs/log"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

type BLSEvidencePool struct {
	*BaseEvidencePool
	dkgInstance dkgTypes.DKG
}

func NewBLSEvidencePool(stateDB, evidenceDB dbm.DB) *BLSEvidencePool {
	evidenceStore := NewEvidenceStore(evidenceDB)
	evpool := &BaseEvidencePool{
		stateDB:       stateDB,
		state:         sm.LoadState(stateDB),
		logger:        log.NewNopLogger(),
		evidenceStore: evidenceStore,
		evidenceList:  clist.New(),
	}
	return &BLSEvidencePool{
		BaseEvidencePool: evpool,
	}
}

func (evpool *BLSEvidencePool) SetDKGInstance(dkgInstance dkgTypes.DKG) {
	evpool.dkgInstance = dkgInstance
}

// AddEvidence checks the evidence is valid and adds it to the pool.
func (evpool *BLSEvidencePool) AddEvidence(evidence types.Evidence) (err error) {

	// TODO: check if we already have evidence for this
	// validator at this height so we dont get spammed

	if err := sm.BLSVerifyEvidence(evpool.stateDB, evpool.State(), evpool.dkgInstance, evidence); err != nil {
		return err
	}

	// fetch the validator and return its voting power as its priority
	// TODO: something better ?
	valset, _ := sm.LoadValidators(evpool.stateDB, evidence.Height())
	_, val := valset.GetByAddress(evidence.Address())
	priority := val.VotingPower

	added := evpool.evidenceStore.AddNewEvidence(evidence, priority)
	if !added {
		// evidence already known, just ignore
		return
	}

	evpool.logger.Info("Verified new evidence of byzantine behaviour", "evidence", evidence)

	// add evidence to clist
	evpool.evidenceList.PushBack(evidence)

	return nil
}
