package consensus

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/dgamingfoundation/dkglib/lib/blsShare"

	dkgalias "github.com/dgamingfoundation/dkglib/lib/alias"
	dkglib "github.com/dgamingfoundation/dkglib/lib/dealer"
	dkgtypes "github.com/dgamingfoundation/dkglib/lib/types"
	"github.com/tendermint/tendermint/alias"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
)

// TODO: implement round timeouts.
// TODO: implement protection from OOM (restrict maximum possible number of active rounds).
// TODO: implement tests.

const (
	BlocksAhead = 20 // Agree to swap verifier after around this number of blocks.
	//DefaultDKGNumBlocks sets how often node should make DKG(in blocks)
	DefaultDKGNumBlocks = 100
)

var (
	ErrDKGVerifierNotReady = errors.New("verifier not ready yet")
)

type dkgState struct {
	mtx sync.RWMutex

	verifier     dkgtypes.Verifier
	nextVerifier dkgtypes.Verifier
	changeHeight int64

	// message queue used for dkgState-related messages.
	dkgMsgQueue      chan msgInfo
	dkgRoundToDealer map[int]dkglib.Dealer
	dkgRoundID       int
	dkgNumBlocks     int64
	newDKGDealer     dkglib.DKGDealerConstructor
	privValidator    alias.PrivValidator

	Logger log.Logger
	evsw   events.EventSwitch
}

func NewDKG(evsw events.EventSwitch, options ...DKGOption) *dkgState {
	dkg := &dkgState{
		evsw:             evsw,
		dkgMsgQueue:      make(chan msgInfo, msgQueueSize),
		dkgRoundToDealer: make(map[int]dkglib.Dealer),
		newDKGDealer:     dkglib.NewDKGDealer,
		dkgNumBlocks:     DefaultDKGNumBlocks,
	}

	for _, option := range options {
		option(dkg)
	}

	if dkg.dkgNumBlocks == 0 {
		dkg.dkgNumBlocks = DefaultDKGNumBlocks // We do not want to panic if the value is not provided.
	}

	return dkg
}

// DKGOption sets an optional parameter on the dkgState.
type DKGOption func(*dkgState)

func WithVerifier(verifier dkgtypes.Verifier) DKGOption {
	return func(d *dkgState) { d.verifier = verifier }
}

func WithDKGNumBlocks(numBlocks int64) DKGOption {
	return func(d *dkgState) { d.dkgNumBlocks = numBlocks }
}

func WithLogger(l log.Logger) DKGOption {
	return func(d *dkgState) { d.Logger = l }
}

func WithPVKey(pv alias.PrivValidator) DKGOption {
	return func(d *dkgState) { d.privValidator = pv }
}

func WithDKGDealerConstructor(newDealer dkglib.DKGDealerConstructor) DKGOption {
	return func(d *dkgState) {
		if newDealer == nil {
			return
		}
		d.newDKGDealer = newDealer
	}
}

func (dkg *dkgState) HandleDKGShare(mi msgInfo, height int64, validators *alias.ValidatorSet, pubKey crypto.PubKey) {
	dkg.mtx.Lock()
	defer dkg.mtx.Unlock()

	dkgMsg, ok := mi.Msg.(*dkgtypes.DKGDataMessage)
	if !ok {
		dkg.Logger.Info("dkgState: rejecting message (unknown type)", reflect.TypeOf(dkgMsg).Name())
		return
	}

	var msg = dkgMsg.Data
	dealer, ok := dkg.dkgRoundToDealer[msg.RoundID]
	if !ok {
		dkg.Logger.Info("dkgState: dealer not found, creating a new dealer", "round_id", msg.RoundID)
		dealer = dkg.newDKGDealer(validators, dkg.privValidator, dkg.sendSignedDKGMessage, dkg.evsw, dkg.Logger, msg.RoundID)
		dkg.dkgRoundToDealer[msg.RoundID] = dealer
		if err := dealer.Start(); err != nil {
			panic(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
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
	case dkgalias.DKGPubKey:
		dkg.Logger.Info("dkgState: received PubKey message", "from", fromAddr)
		err = dealer.HandleDKGPubKey(msg)
	case dkgalias.DKGDeal:
		dkg.Logger.Info("dkgState: received Deal message", "from", fromAddr)
		err = dealer.HandleDKGDeal(msg)
	case dkgalias.DKGResponse:
		dkg.Logger.Info("dkgState: received Response message", "from", fromAddr)
		err = dealer.HandleDKGResponse(msg)
	case dkgalias.DKGJustification:
		dkg.Logger.Info("dkgState: received Justification message", "from", fromAddr)
		err = dealer.HandleDKGJustification(msg)
	case dkgalias.DKGCommits:
		dkg.Logger.Info("dkgState: received Commit message", "from", fromAddr)
		err = dealer.HandleDKGCommit(msg)
	case dkgalias.DKGComplaint:
		dkg.Logger.Info("dkgState: received Complaint message", "from", fromAddr)
		err = dealer.HandleDKGComplaint(msg)
	case dkgalias.DKGReconstructCommit:
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
	if err == dkgtypes.ErrDKGVerifierNotReady {
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
	dkg.evsw.FireEvent(dkgtypes.EventDKGSuccessful, dkg.changeHeight)
}

func (dkg *dkgState) startDKGRound(validators *alias.ValidatorSet) error {
	dkg.dkgRoundID++
	dkg.Logger.Info("dkgState: starting round", "round_id", dkg.dkgRoundID)
	_, ok := dkg.dkgRoundToDealer[dkg.dkgRoundID]
	if !ok {
		dealer := dkg.newDKGDealer(validators, dkg.privValidator, dkg.sendSignedDKGMessage, dkg.evsw, dkg.Logger, dkg.dkgRoundID)
		dkg.dkgRoundToDealer[dkg.dkgRoundID] = dealer
		dkg.evsw.FireEvent(dkgtypes.EventDKGStart, dkg.dkgRoundID)
		return dealer.Start()
	}

	return nil
}

func (dkg *dkgState) sendDKGMessage(msg *dkgalias.DKGData) {
	// Broadcast to peers. This will not lead to processing the message
	// on the sending node, we need to send it manually (see below).
	dkg.evsw.FireEvent(dkgtypes.EventDKGData, msg)
	mi := msgInfo{&dkgtypes.DKGDataMessage{msg}, ""}
	select {
	case dkg.dkgMsgQueue <- mi:
	default:
		dkg.Logger.Info("dkgMsgQueue is full. Using a go-routine")
		go func() { dkg.dkgMsgQueue <- mi }()
	}
}

func (dkg *dkgState) sendSignedDKGMessage(data *dkgalias.DKGData) error {
	if err := dkg.Sign(data); err != nil {
		return err
	}
	dkg.Logger.Info("DKG: msg signed with signature", "signature", hex.EncodeToString(data.Signature))
	dkg.sendDKGMessage(data)
	return nil
}

// Sign sign message by dealer's secret key
func (dkg *dkgState) Sign(data *dkgalias.DKGData) error {
	dkg.privValidator.SignData("rchain", data)
	return nil
}

func (dkg *dkgState) slashDKGLosers(losers []*alias.Validator) {
	for _, loser := range losers {
		dkg.Logger.Info("Slashing validator", loser.Address.String())
	}
}

func (dkg *dkgState) CheckDKGTime(height int64, validators *alias.ValidatorSet) {
	if dkg.changeHeight == height {
		dkg.Logger.Info("dkgState: time to update verifier", dkg.changeHeight, height)
		dkg.verifier, dkg.nextVerifier = dkg.nextVerifier, nil
		dkg.changeHeight = 0
		dkg.evsw.FireEvent(dkgtypes.EventDKGKeyChange, height)
	}

	if height > 1 && height%dkg.dkgNumBlocks == 0 {
		if err := dkg.startDKGRound(validators); err != nil {
			panic(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
		}
	}
}

func (dkg *dkgState) MsgQueue() chan msgInfo {
	return dkg.dkgMsgQueue
}

func (dkg *dkgState) Verifier() dkgtypes.Verifier {
	return dkg.verifier
}

func (dkg *dkgState) SetVerifier(v dkgtypes.Verifier) {
	dkg.verifier = v
}

type verifierFunc func(s string, i int) dkgtypes.Verifier

func GetVerifier(T, N int) verifierFunc {
	return func(s string, i int) dkgtypes.Verifier {
		return blsShare.NewTestBLSVerifierByID(s, i, T, N)
	}
}

func GetMockVerifier() verifierFunc {
	return func(s string, i int) dkgtypes.Verifier {
		return new(dkgtypes.MockVerifier)
	}
}
