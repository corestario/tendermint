package consensus

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"errors"

	"github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/share"
	dkg "go.dedis.ch/kyber/share/dkg/rabin"
	vss "go.dedis.ch/kyber/share/vss/rabin"
)

// TODO: implement round timeouts.
// TODO: implement protection from OOM (restrict maximum possible number of active rounds).
// TODO: implement tests.

const (
	BlocksAhead = 20 // Agree to swap verifier after around this number of blocks.
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
	dkgRoundToDealer map[int]*DKGDealer
	dkgRoundID       int
	dkgNumBlocks     int64

	Logger log.Logger
	evsw events.EventSwitch
}

func NewDKG(evsw events.EventSwitch, options ...DKGOption) *dkgState {
	dkg := &dkgState{
		evsw: evsw,
		dkgMsgQueue:      make(chan msgInfo, msgQueueSize),
		dkgRoundToDealer: make(map[int]*DKGDealer),
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

func (dkg *dkgState) SetVerifier(verifier types.Verifier) {
	dkg.verifier = verifier
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
		dealer = NewDKGDealer(validators, pubKey, dkg.sendDKGMessage, dkg.Logger)
		dkg.dkgRoundToDealer[msg.RoundID] = dealer
		if err := dealer.start(); err != nil {
			common.PanicSanity(fmt.Sprintf("failed to start a dealer (round %d): %v", dkg.dkgRoundID, err))
		}
	}
	if dealer == nil {
		dkg.Logger.Info("dkgState: received message for inactive round:", "round", msg.RoundID)
		return
	}
	fromAddr := crypto.Address(msg.Addr).String()

	var err error
	switch msg.Type {
	case types.DKGPubKey:
		dkg.Logger.Info("dkgState: received PubKey message", "from", fromAddr)
		err = dealer.handleDKGPubKey(msg)
	case types.DKGDeal:
		dkg.Logger.Info("dkgState: received Deal message", "from", fromAddr)
		err = dealer.handleDKGDeal(msg)
	case types.DKGResponse:
		dkg.Logger.Info("dkgState: received Response message", "from", fromAddr)
		err = dealer.handleDKGResponse(msg)
	case types.DKGJustification:
		dkg.Logger.Info("dkgState: received Justification message", "from", fromAddr)
		err = dealer.handleDKGJustification(msg)
	case types.DKGCommits:
		dkg.Logger.Info("dkgState: received Commit message", "from", fromAddr)
		err = dealer.handleDKGCommit(msg)
	case types.DKGComplaint:
		dkg.Logger.Info("dkgState: received Complaint message", "from", fromAddr)
		err = dealer.handleDKGComplaint(msg)
	case types.DKGReconstructCommit:
		dkg.Logger.Info("dkgState: received ReconstructCommit message", "from", fromAddr)
		err = dealer.handleDKGReconstructCommit(msg)
	}
	if err != nil {
		dkg.Logger.Error("dkgState: failed to handle message", "error", err, "type", msg.Type)
		dkg.slashDKGLosers(dealer.getLosers())
		dkg.dkgRoundToDealer[msg.RoundID] = nil
		return
	}

	verifier, err := dealer.getVerifier()
	if err == errDKGVerifierNotReady {
		dkg.Logger.Debug("dkgState: verifier not ready")
		return
	}
	if err != nil {
		dkg.Logger.Error("dkgState: verifier should be ready, but it's not ready:", err)
		dkg.slashDKGLosers(dealer.getLosers())
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
}

func (dkg *dkgState) startDKGRound(validators *types.ValidatorSet, pubKey crypto.PubKey) error {
	dkg.dkgRoundID++
	dkg.Logger.Info("dkgState: starting round", "round_id", dkg.dkgRoundID)
	dealer, ok := dkg.dkgRoundToDealer[dkg.dkgRoundID]
	if !ok {
		dkg.Logger.Info("dkgState: dealer not found, creating a new dealer", "round_id", dkg.dkgRoundID)
		dealer = NewDKGDealer(validators, pubKey, dkg.sendDKGMessage, dkg.Logger)
		dkg.dkgRoundToDealer[dkg.dkgRoundID] = dealer
		return dealer.start()
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

func (dkg *dkgState) slashDKGLosers(losers []*types.Validator) {
	for _, loser := range losers {
		dkg.Logger.Info("Slashing validator", loser.Address.String())
	}
}

func (dkg *dkgState) CheckDKGTime(height int64, validators *types.ValidatorSet, privateValidator types.PrivValidator) {
	if dkg.changeHeight != height {
		dkg.Logger.Info("dkgState: time to update verifier")
		dkg.verifier, dkg.nextVerifier = dkg.nextVerifier, nil
		dkg.changeHeight = 0
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

type DKGDealer struct {
	sendMsgCb  func(*types.DKGData)
	validators *types.ValidatorSet
	addrBytes  []byte
	logger     log.Logger

	participantID int
	roundID       int

	pubKey      kyber.Point
	secKey      kyber.Scalar
	suiteG1     *bn256.Suite
	suiteG2     *bn256.Suite
	instance    *dkg.DistKeyGenerator
	transitions []transition

	pubKeys            PKStore
	deals              map[string]*dkg.Deal
	responses          []*dkg.Response
	justifications     map[string]*dkg.Justification
	commits            []*dkg.SecretCommits
	complaints         []*dkg.ComplaintCommits
	reconstructCommits []*dkg.ReconstructCommits

	losers []crypto.Address
}

func NewDKGDealer(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) *DKGDealer {
	return &DKGDealer{
		validators: validators,
		addrBytes:  pubKey.Address().Bytes(),
		sendMsgCb:  sendMsgCb,
		logger:     logger,
		suiteG1:    bn256.NewSuiteG1(),
		suiteG2:    bn256.NewSuiteG2(),

		deals:          make(map[string]*dkg.Deal),
		justifications: make(map[string]*dkg.Justification),
	}
}

func (m *DKGDealer) start() error {
	m.roundID++
	m.secKey = m.suiteG2.Scalar().Pick(m.suiteG2.RandomStream())
	m.pubKey = m.suiteG2.Point().Mul(m.secKey, nil)
	m.generateTransitions()

	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err := enc.Encode(m.pubKey); err != nil {
		return fmt.Errorf("failed to encode public key: %v", err)
	}

	m.logger.Info("dkgState: sending pub key", "key", m.pubKey.String())
	m.sendMsgCb(&types.DKGData{
		Type:    types.DKGPubKey,
		RoundID: m.roundID,
		Addr:    m.addrBytes,
		Data:    buf.Bytes(),
	})

	return nil
}

func (m *DKGDealer) transit() error {
	for len(m.transitions) > 0 {
		var tn = m.transitions[0]
		err, ready := tn()
		if !ready {
			return nil
		}
		if err != nil {
			return err
		}
		m.transitions = m.transitions[1:]
	}

	return nil
}

func (m *DKGDealer) resetDKGData() {
	m.pubKey = nil
	m.secKey = nil
	m.suiteG1 = nil
	m.suiteG2 = nil
	m.instance = nil
	m.transitions = nil

	m.pubKeys = nil
	m.deals = nil
	m.responses = nil
	m.justifications = nil
	m.commits = nil
	m.complaints = nil
	m.reconstructCommits = nil
}

func (m *DKGDealer) generateTransitions() {
	m.transitions = []transition{
		// Phase I
		m.sendDeals,
		m.processDeals,
		m.processResponses,
		m.processJustifications,
		// Phase II
		m.processCommits,
		m.processComplaints,
		m.processReconstructCommits,
	}
}

func (m *DKGDealer) getLosers() []*types.Validator {
	var out []*types.Validator
	for _, loser := range m.losers {
		_, validator := m.validators.GetByAddress(loser)
		out = append(out, validator)
	}

	return out
}

//////////////////////////////////////////////////////////////////////////////
//
// PHASE I
//
//////////////////////////////////////////////////////////////////////////////

func (m *DKGDealer) handleDKGPubKey(msg *types.DKGData) error {
	var (
		dec    = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		pubKey = m.suiteG2.Point()
	)
	if err := dec.Decode(pubKey); err != nil {
		return fmt.Errorf("dkgState: failed to decode public key from %s: %v", msg.Addr, err)
	}
	m.pubKeys.Add(&PK2Addr{PK: pubKey, Addr: crypto.Address(msg.Addr)})

	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) sendDeals() (err error, ready bool) {
	if len(m.pubKeys) != m.validators.Size() {
		return nil, false
	}
	m.logger.Info("dkgState: sending deals")

	sort.Sort(m.pubKeys)
	dkgInstance, err := dkg.NewDistKeyGenerator(m.suiteG2, m.secKey, m.pubKeys.GetPKs(), (m.validators.Size()*2)/3)
	if err != nil {
		return fmt.Errorf("failed to create dkgState instance: %v", err), true
	}
	m.instance = dkgInstance

	deals, err := m.instance.Deals()
	if err != nil {
		return fmt.Errorf("failed to populate deals: %v", err), true
	}
	for _, deal := range deals {
		m.participantID = int(deal.Index) // Same for each deal.
		break
	}

	for toIndex, deal := range deals {
		var (
			buf = bytes.NewBuffer(nil)
			enc = gob.NewEncoder(buf)
		)
		if err := enc.Encode(deal); err != nil {
			return fmt.Errorf("failed to encode deal #%d: %v", deal.Index, err), true
		}
		m.sendMsgCb(&types.DKGData{
			Type:    types.DKGDeal,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
			Data:    buf.Bytes(),
			ToIndex: toIndex,
		})
	}

	return nil, true
}

func (m *DKGDealer) handleDKGDeal(msg *types.DKGData) error {
	var (
		dec  = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		deal = &dkg.Deal{ // We need to initialize everything down to the kyber.Point to avoid nil panics.
			Deal: &vss.EncryptedDeal{
				DHKey: m.suiteG2.Point(),
			},
		}
	)
	if err := dec.Decode(deal); err != nil {
		return fmt.Errorf("failed to decode deal: %v", err)
	}

	if m.participantID != msg.ToIndex {
		m.logger.Debug("dkgState: rejecting deal (intended for another participant)", "intended", msg.ToIndex)
		return nil
	}

	m.logger.Info("dkgState: deal is intended for us, storing")
	if _, exists := m.deals[msg.GetAddrString()]; exists {
		return nil
	}

	m.deals[msg.GetAddrString()] = deal
	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processDeals() (err error, ready bool) {
	if len(m.deals) < m.validators.Size()-1 {
		return nil, false
	}
	m.logger.Info("dkgState: processing deals")

	for _, deal := range m.deals {
		resp, err := m.instance.ProcessDeal(deal)
		if err != nil {
			return fmt.Errorf("failed to ProcessDeal: %v", err), true
		}
		var (
			buf = bytes.NewBuffer(nil)
			enc = gob.NewEncoder(buf)
		)
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("failed to encode response: %v", err), true
		}
		m.sendMsgCb(&types.DKGData{
			Type:    types.DKGResponse,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
			Data:    buf.Bytes(),
		})
	}

	return nil, true
}

func (m *DKGDealer) handleDKGResponse(msg *types.DKGData) error {
	var (
		dec  = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		resp = &dkg.Response{}
	)
	if err := dec.Decode(resp); err != nil {
		return fmt.Errorf("failed to decode deal: %v", err)
	}

	if uint32(m.participantID) == resp.Response.Index {
		m.logger.Debug("dkgState: skipping response")
		return nil
	}

	m.logger.Info("dkgState: response is intended for us, storing")

	m.responses = append(m.responses, resp)
	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processResponses() (err error, ready bool) {
	if len(m.responses) < (m.validators.Size()-1)*(m.validators.Size()-1) {
		return nil, false
	}
	m.logger.Info("dkgState: processing responses")

	for _, resp := range m.responses {
		var msg = &types.DKGData{
			Type:    types.DKGJustification,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
		}

		// In this call we might or might not put a justification to msg.Data.
		err := func() error {
			if resp.Response.Approved {
				m.logger.Info("dkgState: deal is approved", "to", resp.Index, "from", resp.Response.Index)
			}

			justification, err := m.instance.ProcessResponse(resp)
			if err != nil {
				return fmt.Errorf("failed to ProcessResponse: %v", err)
			}
			if justification == nil {
				return nil
			}

			var (
				buf = bytes.NewBuffer(nil)
				enc = gob.NewEncoder(buf)
			)
			if err := enc.Encode(justification); err != nil {
				return fmt.Errorf("failed to encode response: %v", err)
			}
			msg.Data = buf.Bytes()

			return nil
		}()
		if err != nil {
			return err, true
		}

		m.sendMsgCb(msg)
	}

	return nil, true
}

func (m *DKGDealer) handleDKGJustification(msg *types.DKGData) error {
	var justification *dkg.Justification
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		justification = &dkg.Justification{}
		if err := dec.Decode(justification); err != nil {
			return fmt.Errorf("failed to decode deal: %v", err)
		}
	}

	if _, exists := m.justifications[msg.GetAddrString()]; exists {
		return nil
	}
	m.justifications[msg.GetAddrString()] = justification

	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processJustifications() (err error, ready bool) {
	if len(m.justifications) < m.validators.Size() {
		return nil, false
	}
	m.logger.Info("dkgState: processing justifications")

	for _, justification := range m.justifications {
		if justification != nil {
			m.logger.Info("dkgState: processing non-empty justification", "from", justification.Index)
			if err := m.instance.ProcessJustification(justification); err != nil {
				return fmt.Errorf("failed to ProcessJustification: %v", err), true
			}
		} else {
			m.logger.Info("dkgState: empty justification, everything is o.k.")
		}
	}

	if !m.instance.Certified() {
		return errors.New("instance is not certified"), true
	}

	qual := m.instance.QUAL()
	m.logger.Info("dkgState: got the QUAL set", "qual", qual)
	if len(qual) < m.validators.Size() {
		qualSet := map[int]bool{}
		for _, idx := range qual {
			qualSet[idx] = true
		}

		for idx, pk2addr := range m.pubKeys {
			if !qualSet[idx] {
				m.losers = append(m.losers, pk2addr.Addr)
			}
		}

		return errors.New("some of participants failed to complete phase I"), true
	}

	commits, err := m.instance.SecretCommits()
	if err != nil {
		return fmt.Errorf("failed to get commits: %v", err), true
	}
	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err := enc.Encode(commits); err != nil {
		return fmt.Errorf("failed to encode response: %v", err), true
	}
	m.sendMsgCb(&types.DKGData{
		Type:        types.DKGCommits,
		RoundID:     m.roundID,
		Addr:        m.addrBytes,
		Data:        buf.Bytes(),
		NumEntities: len(commits.Commitments),
	})

	return nil, true
}

//////////////////////////////////////////////////////////////////////////////
//
// PHASE II
//
//////////////////////////////////////////////////////////////////////////////

func (m *DKGDealer) handleDKGCommit(msg *types.DKGData) error {
	dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
	commits := &dkg.SecretCommits{}
	for i := 0; i < msg.NumEntities; i++ {
		commits.Commitments = append(commits.Commitments, m.suiteG2.Point())
	}
	if err := dec.Decode(commits); err != nil {
		return fmt.Errorf("failed to decode commit: %v", err)
	}
	m.commits = append(m.commits, commits)

	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processCommits() (err error, ready bool) {
	if len(m.commits) < len(m.instance.QUAL()) {
		return nil, false
	}
	m.logger.Info("dkgState: processing commits")

	var alreadyFinished = true
	var messages []*types.DKGData
	for _, commits := range m.commits {
		var msg = &types.DKGData{
			Type:    types.DKGComplaint,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
		}
		complaint, err := m.instance.ProcessSecretCommits(commits)
		if err != nil {
			return fmt.Errorf("failed to ProcessSecretCommits: %v", err), true
		}
		if complaint != nil {
			alreadyFinished = false
			var (
				buf = bytes.NewBuffer(nil)
				enc = gob.NewEncoder(buf)
			)
			if err := enc.Encode(complaint); err != nil {
				return fmt.Errorf("failed to encode response: %v", err), true
			}
			msg.Data = buf.Bytes()
			msg.NumEntities = len(complaint.Deal.Commitments)
		}
		messages = append(messages, msg)
	}
	if !alreadyFinished {
		for _, msg := range messages {
			m.sendMsgCb(msg)
		}
	}

	return nil, true
}

func (m *DKGDealer) handleDKGComplaint(msg *types.DKGData) error {
	var complaint *dkg.ComplaintCommits
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		complaint = &dkg.ComplaintCommits{
			Deal: &vss.Deal{},
		}
		for i := 0; i < msg.NumEntities; i++ {
			complaint.Deal.Commitments = append(complaint.Deal.Commitments, m.suiteG2.Point())
		}
		if err := dec.Decode(complaint); err != nil {
			return fmt.Errorf("failed to decode complaint: %v", err)
		}
	}

	m.complaints = append(m.complaints, complaint)

	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processComplaints() (err error, ready bool) {
	if len(m.complaints) < len(m.instance.QUAL())-1 {
		return nil, false
	}
	m.logger.Info("dkgState: processing commits")

	for _, complaint := range m.complaints {
		var msg = &types.DKGData{
			Type:    types.DKGReconstructCommit,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
		}
		if complaint != nil {
			reconstructionMsg, err := m.instance.ProcessComplaintCommits(complaint)
			if err != nil {
				return fmt.Errorf("failed to ProcessComplaintCommits: %v", err), true
			}
			if reconstructionMsg != nil {
				var (
					buf = bytes.NewBuffer(nil)
					enc = gob.NewEncoder(buf)
				)
				if err := enc.Encode(complaint); err != nil {
					return fmt.Errorf("failed to encode response: %v", err), true
				}
				msg.Data = buf.Bytes()
			}
		}
		m.sendMsgCb(msg)
	}

	return nil, true
}

func (m *DKGDealer) handleDKGReconstructCommit(msg *types.DKGData) error {
	var rc *dkg.ReconstructCommits
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		rc = &dkg.ReconstructCommits{}
		if err := dec.Decode(rc); err != nil {
			return fmt.Errorf("failed to decode complaint: %v", err)
		}
	}

	m.reconstructCommits = append(m.reconstructCommits, rc)

	if err := m.transit(); err != nil {
		return fmt.Errorf("failed to transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) processReconstructCommits() (err error, ready bool) {
	if len(m.reconstructCommits) < len(m.instance.QUAL())-1 {
		return nil, false
	}

	for _, rc := range m.reconstructCommits {
		if rc == nil {
			continue
		}
		if err := m.instance.ProcessReconstructCommits(rc); err != nil {
			return fmt.Errorf("failed to ProcessReconstructCommits: %v", err), true
		}
	}

	if !m.instance.Finished() {
		return errors.New("dkgState round is finished, but dkgState instance is not ready"), true
	}

	return nil, true
}

func (m *DKGDealer) getVerifier() (types.Verifier, error) {
	if m.instance == nil || !m.instance.Finished() {
		return nil, errDKGVerifierNotReady
	}

	distKeyShare, err := m.instance.DistKeyShare()
	if err != nil {
		return nil, fmt.Errorf("failed to get DistKeyShare: %v", err)
	}

	var (
		masterPubKey = share.NewPubPoly(bn256.NewSuiteG2(), nil, distKeyShare.Commitments())
		newShare     = &types.BLSShare{
			ID:   m.participantID,
			Pub:  &share.PubShare{I: m.participantID, V: m.pubKey},
			Priv: distKeyShare.PriShare(),
		}
		t, n = (m.validators.Size() / 3) * 2, m.validators.Size()
	)

	return types.NewBLSVerifier(masterPubKey, newShare, t, n), nil
}

type PK2Addr struct {
	Addr crypto.Address
	PK   kyber.Point
}

type PKStore []*PK2Addr

func (m *PKStore) Add(newPk *PK2Addr) bool {
	for _, pk := range *m {
		if pk.Addr.String() == newPk.Addr.String() && pk.PK.Equal(newPk.PK) {
			return false
		}
	}
	*m = append(*m, newPk)

	return true
}

func (m PKStore) Len() int           { return len(m) }
func (m PKStore) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m PKStore) Less(i, j int) bool { return m[i].Addr.String() < m[j].Addr.String() }
func (m PKStore) GetPKs() []kyber.Point {
	var out = make([]kyber.Point, len(m))
	for idx, val := range m {
		out[idx] = val.PK
	}
	return out
}

type transition func() (error, bool)

type Justification struct {
	Void          bool
	Justification *dkg.Justification
}
