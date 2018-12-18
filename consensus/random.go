package consensus

import "github.com/tendermint/tendermint/types"

func (cs *ConsensusState) RestoreRandom(precommits *types.VoteSet) error {
	for _, precommit := range precommits.VoteStrings() {
		cs.Logger.Info("Restoring Random from", "precommit", precommit, "precommits", precommits.StringShort())
	}

	return nil
}
