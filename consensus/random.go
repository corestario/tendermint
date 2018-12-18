package consensus

import (
	"encoding/binary"
	"fmt"
	"math/rand"

	cstypes "github.com/tendermint/tendermint/consensus/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/types"
)

func (cs *ConsensusState) enterRandom(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cstypes.RoundStepRandom <= cs.Step) {
		logger.Debug(fmt.Sprintf("enterRandom(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))
		return
	}

	logger.Info(fmt.Sprintf("enterRandom(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterPrecommit:
		cs.updateRoundStep(round, cstypes.RoundStepRandom)
		cs.newStep()
	}()

	blockID, ok := cs.Votes.Precommits(round).TwoThirdsMajority()
	if !ok {
		cmn.PanicSanity("enterRandom() expects +2/3 precommits")
	}

	randomChunk := make([]byte, 8)
	binary.LittleEndian.PutUint64(randomChunk, uint64(rand.Int63()))
	share := cs.signAddVote(types.RandomType, blockID.Hash, blockID.PartsHeader, randomChunk)
	cs.Logger.Info("Broadcast random share", "height", cs.Height, "round", cs.Round, "share", share)
}

func (cs *ConsensusState) enterRandomWait(height int64, round int) {
	logger := cs.Logger.With("height", height, "round", round)

	if cs.Height != height || round < cs.Round || (cs.Round == round && cs.triggeredTimeoutPrecommit) {
		logger.Debug(
			fmt.Sprintf(
				"enterRandomWait(%v/%v): Invalid args. "+
					"Current state is Height/Round: %v/%v/, triggeredTimeoutPrecommit:%v",
				height, round, cs.Height, cs.Round, cs.triggeredTimeoutPrecommit))
		return
	}
	if !cs.Votes.Randoms(round).HasTwoThirdsAny() {
		cmn.PanicSanity(fmt.Sprintf("enterRandomWait(%v/%v), but Randoms does not have any +2/3 votes", height, round))
	}
	logger.Info(fmt.Sprintf("enterRandomWait(%v/%v). Current: %v/%v/%v", height, round, cs.Height, cs.Round, cs.Step))

	defer func() {
		// Done enterPrecommitWait:
		cs.triggeredTimeoutRandom = true
		cs.newStep()
	}()

	// Wait for some more precommits; enterNewRound
	cs.scheduleTimeout(cs.config.Precommit(round), height, round, cstypes.RoundStepRandomWait)

}

func (cs ConsensusState) computeRandom(randoms *types.VoteSet) error {
	for idx, vote := range randoms.VoteStrings() {
		cs.Logger.Info("Computing random using vote", "vote_idx", idx, "vote", vote)
	}

	return nil
}
