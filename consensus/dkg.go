package consensus

import (
	"reflect"

	"github.com/tendermint/tendermint/libs/common"

	"github.com/tendermint/tendermint/types"
)

func (cs *ConsensusState) handleDKGShare(mi msgInfo) {
	msg, ok := mi.Msg.(*DKGShareMessage)
	if !ok {
		cs.Logger.Info("rejecting dkg share message (unknown type): %v", reflect.TypeOf(msg).Name())
		return
	}

	var share = msg.Share

	if !cs.dkgRoundActive {
		cs.Logger.Info("rejecting dkg share message (not inside DKG iteration): %v", share)
		return
	}

	if share.RoundID != cs.dkgRoundID {
		cs.Logger.Info("rejecting dkg share message (invalid DKG round id): %v (has %d, want %d)",
			share, cs.dkgRoundID, share.RoundID)
		return
	}

	cs.dkgShares = append(cs.dkgShares, share)

	if len(cs.dkgShares) >= len(cs.Validators.Validators) {
		if err := cs.buildNewVerifier(); err != nil {
			cs.Logger.Info("failed to build new verifier: %v", err)
			common.PanicSanity("failed to build new verifier")
		}
		cs.finishDKGRound()
	}
}

func (cs *ConsensusState) sendDKGShare() {
	share := cs.produceDKGShare()
	// Broadcast to peers. This will not lead to processing the message
	// on the sending node, we need to send it manually (see below).
	cs.evsw.FireEvent(types.EventDKGSHare, share)
	mi := msgInfo{&DKGShareMessage{share}, ""}
	select {
	case cs.dkgMsgQueue <- mi:
	default:
		cs.Logger.Info("dkgMsgQueue is full. Using a go-routine")
		go func() { cs.dkgMsgQueue <- mi }()
	}
}

func (cs *ConsensusState) buildNewVerifier() error {
	cs.Logger.Info("producing new validator")
	return nil
}

func (cs *ConsensusState) produceDKGShare() *types.DKGShare {
	return &types.DKGShare{UserID: cs.dkgID, RoundID: cs.dkgRoundID}
}

func (cs *ConsensusState) startDKGRound() bool {
	if cs.dkgRoundActive {
		return false
	}

	cs.dkgRoundID++
	cs.dkgRoundActive = true

	cs.sendDKGShare()

	return true
}

func (cs *ConsensusState) finishDKGRound() {
	cs.dkgLastValidators = cs.Validators
	cs.dkgRoundActive = false

	if cs.dkgStopTheWorld {
		cs.dkgStopTheWorld = false
		cs.dkgStopTheWorldCh <- struct{}{}
	}
}

func (cs *ConsensusState) getValidatorsRatio(oldValidators, newValidators *types.ValidatorSet) float64 {
	if cs.dkgRoundID > 0 && cs.dkgRoundID%2 == 0 {

		return 0.5
	}
	set := make(map[string]struct{})
	for _, validator := range oldValidators.Validators {
		set[validator.Address.String()] = struct{}{}
	}
	for _, validator := range newValidators.Validators {
		set[validator.Address.String()] = struct{}{}
	}

	return float64(len(set)) / float64(len(oldValidators.Validators))
}
