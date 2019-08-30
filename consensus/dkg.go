package consensus

import (
	"encoding/hex"
	"fmt"
	"time"

	"errors"
	"reflect"

	"sync"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

// TODO: implement round timeouts.
// TODO: implement protection from OOM (restrict maximum possible number of active rounds).
// TODO: implement tests.

const (
	BlocksAhead = 20 // Agree to swap verifier after around this number of blocks.
	//DefaultDKGNumBlocks sets how often node should make DKG(in blocks)
	DefaultDKGNumBlocks = 100
	DKGRoundGCTime      = time.Second * 10
	DefaultDKGRoundTTL  = time.Second * 120
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
	dkgRoundToDealer map[uint64]Dealer
	dkgRoundID       uint64
	dkgNumBlocks     int64
	newDKGDealer     DKGDealerConstructor
	privValidator    types.PrivValidator
	roundTTL         time.Duration

	Logger log.Logger
	evsw   events.EventSwitch

	verifierOnce     sync.Once
	verifierMtx      sync.RWMutex
	verifierCallback func(v types.Verifier)

	evidencePool
}

func NewDKG(evsw events.EventSwitch, options ...DKGOption) *dkgState {
	dkg := &dkgState{
		evsw:             evsw,
		dkgMsgQueue:      make(chan msgInfo, msgQueueSize),
		dkgRoundToDealer: make(map[uint64]Dealer),
		newDKGDealer:     NewDKGDealer,
		dkgNumBlocks:     DefaultDKGNumBlocks,
		roundTTL:         DefaultDKGRoundTTL,
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

func WithVerifier(verifier types.Verifier) DKGOption {
	return func(d *dkgState) { d.verifier = verifier }
}

func WithDKGNumBlocks(numBlocks int64) DKGOption {
	return func(d *dkgState) { d.dkgNumBlocks = numBlocks }
}

func WithDKGRoundTTL(timeout time.Duration) DKGOption {
	return func(d *dkgState) { d.roundTTL = timeout }
}

func WithLogger(l log.Logger) DKGOption {
	return func(d *dkgState) { d.Logger = l }
}

func WithPVKey(pv types.PrivValidator) DKGOption {
	return func(d *dkgState) { d.privValidator = pv }
}

func WithEvidencePool(evPool evidencePool) DKGOption {
	return func(d *dkgState) { d.evidencePool = evPool }
}

func WithDKGDealerConstructor(newDealer DKGDealerConstructor) DKGOption {
	return func(d *dkgState) {
		if newDealer == nil {
			return
		}
		d.newDKGDealer = newDealer
	}
}

func (dkg *dkgState) HandleDKGShare(mi msgInfo, height int64, validators *types.ValidatorSet, pubKey crypto.PubKey) bool {
	dkg.mtx.Lock()
	defer dkg.mtx.Unlock()

	dkgMsg, ok := mi.Msg.(*DKGDataMessage)
	if !ok {
		dkg.Logger.Info("dkgState: rejecting message (unknown type)", reflect.TypeOf(dkgMsg).Name())
		return false
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
		return false
	}
	dkg.Logger.Info("dkgState: received message with signature:", "signature", hex.EncodeToString(dkgMsg.Data.Signature))

	if err := dealer.VerifyMessage(*dkgMsg); err != nil {
		dkg.Logger.Info("DKG: can't verify message:", "error", err.Error())
		return false
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
		if dkg.evidencePool != nil {
			ev := &types.DKGMessageEvidence{
				PubKey:   pubKey,
				Height_:  height,
				Error:    err.Error(),
				DataType: int16(msg.Type),
			}
			err := dkg.evidencePool.AddEvidence(ev)
			if err != nil {
				dkg.Logger.Error("dkgState: failed to add evidence to evidencePool", "error", err)
			}
		}
		dkg.slashDKGLosers(dealer.GetLosers())
		dkg.dkgRoundToDealer[msg.RoundID] = nil
		return false
	}

	verifier, err := dealer.GetVerifier()
	if err == errDKGVerifierNotReady {
		dkg.Logger.Debug("dkgState: verifier not ready")
		return false
	}
	if err != nil {
		dkg.Logger.Error("dkgState: verifier should be ready, but it's not ready:", err)
		dkg.slashDKGLosers(dealer.GetLosers())
		dkg.dkgRoundToDealer[msg.RoundID] = nil
		return false
	}

	dkg.verifierOnce.Do(func() {
		dkg.verifierCallback(verifier)
	})
	dkg.Logger.Info("dkgState: verifier is ready, killing older rounds")
	for roundID := range dkg.dkgRoundToDealer {
		if roundID < msg.RoundID {
			dkg.dkgRoundToDealer[msg.RoundID] = nil
		}
	}
	dkg.nextVerifier = verifier
	dkg.changeHeight = (height + BlocksAhead) - ((height + BlocksAhead) % 5)
	dkg.evsw.FireEvent(types.EventDKGSuccessful, dkg.changeHeight)
	return true
}

func (dkg *dkgState) StartRoundsGC() {
	ticker := time.NewTicker(DKGRoundGCTime)
	defer ticker.Stop()

	dkg.Logger.Info("dkgState: starting rounds GC")
	for {
		// No need to add a context for cancelling this routine (ConsensusState itself doesn't
		// have a stopper).
		select {
		case <-ticker.C:
			if dkg.Verifier() != nil {
				dkg.Logger.Info("dkgState: looking for dead rounds", "active_rounds", len(dkg.dkgRoundToDealer))
				for roundID, dealer := range dkg.dkgRoundToDealer {
					// TODO: add Deactivate() and IsDeactivated() call to dealer interface.
					if dealer != nil {
						if time.Now().Sub(dealer.TS()) > dkg.roundTTL {
							dkg.mtx.Lock()
							dkg.dkgRoundToDealer[roundID] = nil
							dkg.mtx.Unlock()
							dkg.Logger.Info("DKG: round killed by timeout", "round_id", roundID)
						}
					}
				}
			}
		}
	}
}

func (dkg *dkgState) StartDKGRound(validators *types.ValidatorSet) error {
	dkg.dkgRoundID++
	dkg.Logger.Info("dkgState: starting round", "round_id", dkg.dkgRoundID)
	_, ok := dkg.dkgRoundToDealer[dkg.dkgRoundID]
	if !ok {
		dealer := dkg.newDKGDealer(validators, dkg.privValidator, dkg.sendSignedDKGMessage, dkg.evsw, dkg.Logger, dkg.dkgRoundID)
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
func (dkg *dkgState) CheckDKGTime(height int64, validators *types.ValidatorSet) {
	if dkg.changeHeight == height {
		dkg.Logger.Info("dkgState: time to update verifier", dkg.changeHeight, height)
		dkg.verifier, dkg.nextVerifier = dkg.nextVerifier, nil
		dkg.changeHeight = 0
		dkg.evsw.FireEvent(types.EventDKGKeyChange, height)
	}

	if height > 1 && height%dkg.dkgNumBlocks == 0 {
		if err := dkg.StartDKGRound(validators); err != nil {
			panic(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
		}
	}
}

func (dkg *dkgState) MsgQueue() chan msgInfo {
	return dkg.dkgMsgQueue
}

func (dkg *dkgState) Verifier() types.Verifier {
	dkg.verifierMtx.RLock()
	defer dkg.verifierMtx.RUnlock()
	return dkg.verifier
}

func (dkg *dkgState) SetVerifier(v types.Verifier) {
	dkg.verifierMtx.Lock()
	defer dkg.verifierMtx.Unlock()
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
