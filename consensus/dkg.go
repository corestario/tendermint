package consensus

import (
	"reflect"

	"github.com/tendermint/tendermint/types"
)

func (cs *ConsensusState) handleDKGShare(mi msgInfo) {
	if !cs.dkgRoundActive {
		cs.Logger.Info("rejecting dkg message (not inside DKG iteration): %v", mi.Msg)
		return
	}

	dkgMsg, ok := mi.Msg.(*DKGMessageMessage)
	if !ok {
		cs.Logger.Info("rejecting dkg message (unknown type): %v", reflect.TypeOf(dkgMsg).Name())
		return
	}

	var msg = dkgMsg.Share
	if msg.RoundID != cs.dkgRoundID {
		cs.Logger.Info("rejecting dkg message (invalid DKG round id): %v (has %d, want %d)",
			msg, cs.dkgRoundID, msg.RoundID)
		return
	}

	switch msg.Type {
	case types.DKGDeal:
		// pass
	case types.DKGResponse:
		// pass
	case types.DKGJustification:
		// pass
	case types.DKGCommit:
		// pass
	case types.DKGComplaint:
		// pass
	case types.DKGReconstructCommit:
		// pass
	}

	cs.finishDKGRound()
}

func (cs *ConsensusState) sendDKGMessage(msg *types.DKGMessage) {
	// Broadcast to peers. This will not lead to processing the message
	// on the sending node, we need to send it manually (see below).
	cs.evsw.FireEvent(types.EventDKGMessage, msg)
	mi := msgInfo{&DKGMessageMessage{msg}, ""}
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

func (cs *ConsensusState) startDKGRound() bool {
	if cs.dkgRoundActive {
		return false
	}

	cs.dkgParticipantID, _ = cs.Validators.GetByAddress(cs.privValidator.GetPubKey().Bytes())

	cs.dkgRoundID++
	cs.dkgRoundActive = true
	// Deal will be produced by actual DKG implementation, mock for now.
	var deal = &types.DKGMessage{
		Type:          types.DKGDeal,
		RoundID:       cs.dkgRoundID,
		ParticipantID: cs.dkgParticipantID,
	}
	cs.sendDKGMessage(deal)

	return true
}

func (cs *ConsensusState) finishDKGRound() {
	cs.dkgRoundActive = false
}
