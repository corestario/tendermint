package consensus

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"

	"github.com/tendermint/tendermint/libs/common"

	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/share"
	dkg "go.dedis.ch/kyber/share/dkg/rabin"
	vss "go.dedis.ch/kyber/share/vss/rabin"
	"go.dedis.ch/kyber/sign/schnorr"
)

// TODO: implement round timeouts.
// TODO: implement protection from OOM (restrict maximum possible number of active rounds).
// TODO: implement tests.

const (
	BlocksAhead = 20
)

var (
	errDKGVerifierNotReady = errors.New("verifier not ready yet")
	errPKStorePKNotFound   = errors.New("There is no PK for this address")
)

func (cs *ConsensusState) handleDKGShare(mi msgInfo) {
	cs.mtx.Lock()
	defer cs.mtx.Unlock()

	dkgMsg, ok := mi.Msg.(*DKGDataMessage)
	if !ok {
		cs.Logger.Info("DKG: rejecting message (unknown type)", reflect.TypeOf(dkgMsg).Name())
		return
	}

	var msg = dkgMsg.Data
	dealer, ok := cs.dkgRoundToDealer[msg.RoundID]
	if !ok {
		cs.Logger.Info("DKG: dealer not found, creating a new dealer", "round_id", msg.RoundID)
		dealer = NewDKGDealer(cs.Validators, cs.privValidator.GetPubKey(), cs.sendDKGMessage, cs.Logger)
		cs.dkgRoundToDealer[msg.RoundID] = dealer
		if err := dealer.start(); err != nil {
			common.PanicSanity(fmt.Sprintf("failed to start a dealer (round %d): %v", cs.dkgRoundID, err))
		}
	}
	if dealer == nil {
		cs.Logger.Info("DKG: received message for inactive round:", "round", msg.RoundID)
		return
	}

	if dkgMsg.Data.Type != types.DKGPubKey {
		cs.Logger.Info("DKG: received message with signature:", "signature", hex.EncodeToString(dkgMsg.Data.Signature))
		if err := dealer.VerifyMessage(*dkgMsg); err != nil {
			if err == errPKStorePKNotFound {
				cs.Logger.Info("DKG: "+err.Error(), "address", dkgMsg.Data.GetAddrString())
				return
			}
			cs.Logger.Info("DKG: received message with invalid signature:", "signature", hex.EncodeToString(dkgMsg.Data.Signature))
			return
		}
		cs.Logger.Info("DKG: message verified")
	}

	fromAddr := crypto.Address(msg.Addr).String()

	var err error
	switch msg.Type {
	case types.DKGPubKey:
		cs.Logger.Info("DKG: received PubKey message", "from", fromAddr)
		err = dealer.handleDKGPubKey(msg)
	case types.DKGDeal:
		cs.Logger.Info("DKG: received Deal message", "from", fromAddr)
		err = dealer.handleDKGDeal(msg)
	case types.DKGResponse:
		cs.Logger.Info("DKG: received Response message", "from", fromAddr)
		err = dealer.handleDKGResponse(msg)
	case types.DKGJustification:
		cs.Logger.Info("DKG: received Justification message", "from", fromAddr)
		err = dealer.handleDKGJustification(msg)
	case types.DKGCommits:
		cs.Logger.Info("DKG: received Commit message", "from", fromAddr)
		err = dealer.handleDKGCommit(msg)
	case types.DKGComplaint:
		cs.Logger.Info("DKG: received Complaint message", "from", fromAddr)
		err = dealer.handleDKGComplaint(msg)
	case types.DKGReconstructCommit:
		cs.Logger.Info("DKG: received ReconstructCommit message", "from", fromAddr)
		err = dealer.handleDKGReconstructCommit(msg)
	}
	if err != nil {
		cs.Logger.Error("DKG: failed to handle message", "error", err, "type", msg.Type)
		cs.slashDKGLosers(dealer.getLosers())
		cs.dkgRoundToDealer[msg.RoundID] = nil
	}

	verifier, err := dealer.getVerifier()
	if err == errDKGVerifierNotReady {
		return
	}
	if err != nil {
		cs.Logger.Error("DKG: verifier should be ready, but it's not ready:", err)
		cs.slashDKGLosers(dealer.getLosers())
		cs.dkgRoundToDealer[msg.RoundID] = nil
	}
	cs.Logger.Info("DKG: verifier is ready, killing older rounds")
	for roundID := range cs.dkgRoundToDealer {
		if roundID < msg.RoundID {
			cs.dkgRoundToDealer[msg.RoundID] = nil
		}
	}
	cs.nextVerifier = verifier
	cs.changeHeight = (cs.Height + BlocksAhead) - ((cs.Height + BlocksAhead) % 5)
}

func (cs *ConsensusState) startDKGRound() error {
	cs.Logger.Info("DKG: starting round", "round_id", cs.dkgRoundID)
	cs.dkgRoundID++
	dealer, ok := cs.dkgRoundToDealer[cs.dkgRoundID]
	if !ok {
		cs.Logger.Info("DKG: dealer not found, creating a new dealer", "round_id", cs.dkgRoundID)
		dealer = NewDKGDealer(cs.Validators, cs.privValidator.GetPubKey(), cs.sendDKGMessage, cs.Logger)
		cs.dkgRoundToDealer[cs.dkgRoundID] = dealer
		return dealer.start()
	}

	return nil
}

func (cs *ConsensusState) sendDKGMessage(msg *types.DKGData) {
	// Broadcast to peers. This will not lead to processing the message
	// on the sending node, we need to send it manually (see below).
	cs.evsw.FireEvent(types.EventDKGData, msg)
	mi := msgInfo{&DKGDataMessage{msg}, ""}
	select {
	case cs.dkgMsgQueue <- mi:
	default:
		cs.Logger.Info("dkgMsgQueue is full. Using a go-routine")
		go func() { cs.dkgMsgQueue <- mi }()
	}
}

func (cs *ConsensusState) slashDKGLosers(losers []*types.Validator) {
	for _, loser := range losers {
		cs.Logger.Info("Slashing validator", loser.Address.String())
	}
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

	m.logger.Info("DKG: sending pub key", "key", m.pubKey.String())
	m.sendMsgCb(&types.DKGData{
		Type:    types.DKGPubKey,
		RoundID: m.roundID,
		Addr:    m.addrBytes,
		Data:    buf.Bytes(),
	})

	return nil
}

func (m *DKGDealer) sendSignedMsg(data *types.DKGData) {
	if err := m.Sign(data); err != nil {
		panic(err)
	}
	m.logger.Info("DKG: msg signed with signature", "signature", hex.EncodeToString(data.Signature))
	m.sendMsgCb(data)
}

//Sign sign message by dealer's secret key
func (m *DKGDealer) Sign(data *types.DKGData) error {
	var (
		sig []byte
		err error
	)
	if sig, err = schnorr.Sign(bn256.NewSuiteG2(), m.secKey, data.SignBytes()); err != nil {
		return err
	}
	data.Signature = sig
	return nil
}

//VerifyMessage verify message by signature
func (m *DKGDealer) VerifyMessage(msg DKGDataMessage) error {
	var (
		pk  *PK2Addr
		err error
	)
	if pk, err = m.pubKeys.FindByAddress(msg.Data.GetAddrString()); err != nil {
		return err
	}
	return schnorr.Verify(bn256.NewSuiteG2(), pk.PK, msg.Data.SignBytes(), msg.Data.Signature)
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
		return fmt.Errorf("DKG: failed to decode public key from %s: %v", msg.Addr, err)
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
	m.logger.Info("DKG: sending deals")

	sort.Sort(m.pubKeys)
	dkgInstance, err := dkg.NewDistKeyGenerator(m.suiteG2, m.secKey, m.pubKeys.GetPKs(), (m.validators.Size()*2)/3)
	if err != nil {
		return fmt.Errorf("failed to create DKG instance: %v", err), true
	}
	m.instance = dkgInstance

	deals, err := m.instance.Deals()
	if err != nil {
		return fmt.Errorf("failed to populate deals: %v", err), true
	}
	for _, deal := range deals {
		m.participantID = int(deal.Index)
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
		m.sendSignedMsg(&types.DKGData{
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
		m.logger.Debug("DKG: rejecting deal (intended for another participant)", "intended", msg.ToIndex)
		return nil
	}

	m.logger.Info("DKG: deal is intended for us, storing")
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
	m.logger.Info("DKG: processing deals")

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
		m.sendSignedMsg(&types.DKGData{
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
		m.logger.Debug("DKG: skipping response")
		return nil
	}

	m.logger.Info("DKG: response is intended for us, storing")

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
	m.logger.Info("DKG: processing responses")

	for _, resp := range m.responses {
		var msg = &types.DKGData{
			Type:    types.DKGJustification,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
		}

		// In this call we might or might not put a justification to msg.Data.
		err := func() error {
			if resp.Response.Approved {
				m.logger.Info("DKG: deal is approved", "to", resp.Index, "from", resp.Response.Index)
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

		m.sendSignedMsg(msg)
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
	m.logger.Info("DKG: processing justifications")

	for _, justification := range m.justifications {
		if justification != nil {
			m.logger.Info("DKG: processing non-empty justification", "from", justification.Index)
			if err := m.instance.ProcessJustification(justification); err != nil {
				return fmt.Errorf("failed to ProcessJustification: %v", err), true
			}
		} else {
			m.logger.Info("DKG: empty justification, everything is o.k.")
		}
	}

	if !m.instance.Certified() {
		return errors.New("instance is not certified"), true
	}

	qual := m.instance.QUAL()
	m.logger.Info("DKG: got the QUAL set", "qual", qual)
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
	m.sendSignedMsg(&types.DKGData{
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
	if len(m.commits) < m.validators.Size() {
		return nil, false
	}
	m.logger.Info("DKG: processing commits")

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
			m.sendSignedMsg(msg)
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
	if len(m.complaints) < m.validators.Size()-1 {
		return nil, false
	}
	m.logger.Info("DKG: processing commits")

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
		m.sendSignedMsg(msg)
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
	if len(m.reconstructCommits) < m.validators.Size()-1 {
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
		return errors.Errorf("dkg round is finished, but dkg instance is not ready"), true
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

//FindByAddress find first pk by given address
func (m PKStore) FindByAddress(addr string) (*PK2Addr, error) {
	size := len(m)
	searchFunc := func(i int) bool {
		return m[i].Addr.String() >= addr
	}
	index := sort.Search(size, searchFunc)
	if index == size {
		return nil, errPKStorePKNotFound
	}
	return m[index], nil
}

type transition func() (error, bool)

type Justification struct {
	Void          bool
	Justification *dkg.Justification
}
