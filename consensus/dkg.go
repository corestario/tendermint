package consensus

import (
	"encoding/hex"
	"errors"
	"fmt"

	"reflect"
	"sync"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

// TODO: implement round timeouts.
// TODO: implement protection from OOM (restrict maximum possible number of active rounds).
// TODO: implement tests.

const (
	BlocksAhead         = 20 // Agree to swap verifier after around this number of blocks.
	//DefaultDKGNumBlocks sets how often node should make DKG(in blocks)
	DefaultDKGNumBlocks = 100
)

var (
	errDKGVerifierNotReady = errors.New("verifier not ready yet")
)

type dkgState struct {
	mtx sync.RWMutex

	verifier     types.Verifier
	nextVerifier types.Verifier
	changeHeight int64

	// message queue used for dkgState-related messages.
	dkgMsgQueue      chan msgInfo
	dkgRoundToDealer map[int]Dealer
	dkgRoundID       int
	dkgNumBlocks     int64
	newDKGDealer     DKGDealerConstructor
	privValidator    types.PrivValidator

	Logger log.Logger
	evsw   events.EventSwitch
}

func NewDKG(evsw events.EventSwitch, options ...DKGOption) *dkgState {
	dkg := &dkgState{
		evsw:             evsw,
		dkgMsgQueue:      make(chan msgInfo, msgQueueSize),
		dkgRoundToDealer: make(map[int]Dealer),
		newDKGDealer:     NewDKGDealer,
		dkgNumBlocks:     DefaultDKGNumBlocks,
	}

	for _, option := range options {
		option(dkg)
	}

	if dkg.dkgNumBlocks == 0 {
		dkg.dkgNumBlocks = 1 // We do not want to panic if the value is not provided.
	}

	return dkg
}

// DKGOption sets an optional parameter on the dkgState.
type DKGOption func(*dkgState)

func WithVerifier(verifier types.Verifier) DKGOption {
	return func(d *dkgState) { d.verifier = verifier }
}

func WithDKGNumBlocks(numBlocks int64) DKGOption {
	return func(d *dkgState) { d.dkgNumBlocks = numBlocks }
}

func WithLogger(l log.Logger) DKGOption {
	return func(d *dkgState) { d.Logger = l }
}

func WithPVKey(pv types.PrivValidator) DKGOption {
	return func(d *dkgState) { d.privValidator = pv }
}

func WithDKGDealerConstructor(newDealer DKGDealerConstructor) DKGOption {
	return func(d *dkgState) {
		if newDealer == nil {
			return
		}
		d.newDKGDealer = newDealer
	}
}

func (dkg *dkgState) HandleDKGShare(mi msgInfo, height int64, validators *types.ValidatorSet, pubKey crypto.PubKey) {
	dkg.mtx.Lock()
	defer dkg.mtx.Unlock()

	dkgMsg, ok := mi.Msg.(*DKGDataMessage)
	if !ok {
		dkg.Logger.Info("dkgState: rejecting message (unknown type)", reflect.TypeOf(dkgMsg).Name())
		return
	}

	var msg = dkgMsg.Data
	dealer, ok := dkg.dkgRoundToDealer[msg.RoundID]
	if !ok {
		dkg.Logger.Info("dkgState: dealer not found, creating a new dealer", "round_id", msg.RoundID)
		dealer = dkg.newDKGDealer(validators, dkg.privValidator, dkg.sendSignedDKGMessage, dkg.evsw, dkg.Logger)
		dkg.dkgRoundToDealer[msg.RoundID] = dealer
		if err := dealer.Start(); err != nil {
			common.PanicSanity(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
		}
	}
	if dealer == nil {
		dkg.Logger.Info("dkgState: received message for inactive round:", "round", msg.RoundID)
		return
	}
	dkg.Logger.Info("dkgState: received message with signature:", "signature", hex.EncodeToString(dkgMsg.Data.Signature))

	if err := dealer.VerifyMessage(*dkgMsg); err != nil {
		dkg.Logger.Info("DKG: can't verify message:", "error", err.Error())
		return
	}
	dkg.Logger.Info("DKG: message verified")

	fromAddr := crypto.Address(msg.Addr).String()

	var err error
	switch msg.Type {
	case types.DKGPubKey:
		dkg.Logger.Info("dkgState: received PubKey message", "from", fromAddr)
		err = dealer.HandleDKGPubKey(msg)
	case types.DKGDeal:
		dkg.Logger.Info("dkgState: received Deal message", "from", fromAddr)
		err = dealer.HandleDKGDeal(msg)
	case types.DKGResponse:
		dkg.Logger.Info("dkgState: received Response message", "from", fromAddr)
		err = dealer.HandleDKGResponse(msg)
	case types.DKGJustification:
		dkg.Logger.Info("dkgState: received Justification message", "from", fromAddr)
		err = dealer.HandleDKGJustification(msg)
	case types.DKGCommits:
		dkg.Logger.Info("dkgState: received Commit message", "from", fromAddr)
		err = dealer.HandleDKGCommit(msg)
	case types.DKGComplaint:
		dkg.Logger.Info("dkgState: received Complaint message", "from", fromAddr)
		err = dealer.HandleDKGComplaint(msg)
	case types.DKGReconstructCommit:
		dkg.Logger.Info("dkgState: received ReconstructCommit message", "from", fromAddr)
		err = dealer.HandleDKGReconstructCommit(msg)
	}
	if err != nil {
		dkg.Logger.Error("dkgState: failed to handle message", "error", err, "type", msg.Type)
		dkg.slashDKGLosers(dealer.GetLosers())
		dkg.dkgRoundToDealer[msg.RoundID] = nil
		return
	}

	verifier, err := dealer.GetVerifier()
	if err == errDKGVerifierNotReady {
		dkg.Logger.Debug("dkgState: verifier not ready")
		return
	}
	if err != nil {
		dkg.Logger.Error("dkgState: verifier should be ready, but it's not ready:", err)
		dkg.slashDKGLosers(dealer.GetLosers())
		dkg.dkgRoundToDealer[msg.RoundID] = nil
		return
	}
	dkg.Logger.Info("dkgState: verifier is ready, killing older rounds")
	for roundID := range dkg.dkgRoundToDealer {
		if roundID < msg.RoundID {
			dkg.dkgRoundToDealer[msg.RoundID] = nil
		}
	}
	dkg.nextVerifier = verifier
	dkg.changeHeight = (height + BlocksAhead) - ((height + BlocksAhead) % 5)
	dkg.evsw.FireEvent(types.EventDKGSuccessful, dkg.changeHeight)

}

func (dkg *dkgState) startDKGRound(validators *types.ValidatorSet, pubKey crypto.PubKey) error {
	dkg.dkgRoundID++
	dkg.Logger.Info("dkgState: starting round", "round_id", dkg.dkgRoundID)
	dealer, ok := dkg.dkgRoundToDealer[dkg.dkgRoundID]
	if !ok {
		dealer = dkg.newDKGDealer(validators, dkg.privValidator, dkg.sendSignedDKGMessage, dkg.evsw, dkg.Logger)
		dkg.dkgRoundToDealer[dkg.dkgRoundID] = dealer
		dkg.evsw.FireEvent(types.EventDKGStart, dkg.dkgRoundID)
		return dealer.Start()
	}

	return nil
}

func (dkg *dkgState) sendDKGMessage(msg *types.DKGData) {
	// Broadcast to peers. This will not lead to processing the message
	// on the sending node, we need to send it manually (see below).
	dkg.evsw.FireEvent(types.EventDKGData, msg)
	mi := msgInfo{&DKGDataMessage{msg}, ""}
	select {
	case dkg.dkgMsgQueue <- mi:
	default:
		dkg.Logger.Info("dkgMsgQueue is full. Using a go-routine")
		go func() { dkg.dkgMsgQueue <- mi }()
	}
}

func (dkg *dkgState) sendSignedDKGMessage(data *types.DKGData) error {
	if err := dkg.Sign(data); err != nil {
		return err
	}
	dkg.Logger.Info("DKG: msg signed with signature", "signature", hex.EncodeToString(data.Signature))
	dkg.sendDKGMessage(data)
	return nil
}

// Sign sign message by dealer's secret key
func (dkg *dkgState) Sign(data *types.DKGData) error {
	return dkg.privValidator.SignDKGData(data)
}

func (dkg *dkgState) slashDKGLosers(losers []*types.Validator) {
	for _, loser := range losers {
		dkg.Logger.Info("Slashing validator", loser.Address.String())
	}
}

func (dkg *dkgState) CheckDKGTime(height int64, validators *types.ValidatorSet, privateValidator types.PrivValidator) {
	if dkg.changeHeight == height {
		dkg.Logger.Info("dkgState: time to update verifier", dkg.changeHeight, height)
		dkg.verifier, dkg.nextVerifier = dkg.nextVerifier, nil
		dkg.changeHeight = 0
		dkg.evsw.FireEvent(types.EventDKGKeyChange, height)
	}

	if height > 1 && height%dkg.dkgNumBlocks == 0 {
		if err := dkg.startDKGRound(validators, privateValidator.GetPubKey()); err != nil {
			common.PanicSanity(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
		}
	}
}

func (dkg *dkgState) MsgQueue() chan msgInfo {
	return dkg.dkgMsgQueue
}

func (dkg *dkgState) Verifier() types.Verifier {
	return dkg.verifier
}

func (dkg *dkgState) SetVerifier(v types.Verifier) {
	dkg.verifier = v
}

type verifierFunc func(s string, i int) types.Verifier

func GetVerifier(T, N int) verifierFunc {
	return func(s string, i int) types.Verifier {
		return types.NewTestBLSVerifierByID(s, i, T, N)
	}
}

func GetMockVerifier() verifierFunc {
	return func(s string, i int) types.Verifier {
		return new(types.MockVerifier)
	}
}
