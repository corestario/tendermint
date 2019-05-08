package consensus

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"sort"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/share"
	dkg "go.dedis.ch/kyber/share/dkg/rabin"
	vss "go.dedis.ch/kyber/share/vss/rabin"
)

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

type DKGDealerConstructor func(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer

func NewDKGDealer(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
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

func (m *DKGDealer) Start() error {
	m.roundID++
	m.secKey = m.suiteG2.Scalar().Pick(m.suiteG2.RandomStream())
	m.pubKey = m.suiteG2.Point().Mul(m.secKey, nil)
	m.GenerateTransitions()

	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err := enc.Encode(m.pubKey); err != nil {
		return fmt.Errorf("failed to encode public key: %v", err)
	}

	m.logger.Info("dkgState: sending pub key", "key", m.pubKey.String())
	m.SendMsgCb(&types.DKGData{
		Type:    types.DKGPubKey,
		RoundID: m.roundID,
		Addr:    m.addrBytes,
		Data:    buf.Bytes(),
	})

	return nil
}

func (m *DKGDealer) Transit() error {
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

func (m *DKGDealer) ResetDKGData() {
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

func (m *DKGDealer) GenerateTransitions() {
	m.transitions = []transition{
		// Phase I
		m.SendDeals,
		m.ProcessDeals,
		m.ProcessResponses,
		m.ProcessJustifications,
		// Phase II
		m.ProcessCommits,
		m.ProcessComplaints,
		m.ProcessReconstructCommits,
	}
}

func (m *DKGDealer) SetTransitions(t []transition) {
	m.transitions = t
}

func (m *DKGDealer) GetLosers() []*types.Validator {
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

func (m *DKGDealer) HandleDKGPubKey(msg *types.DKGData) error {
	var (
		dec    = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		pubKey = m.suiteG2.Point()
	)
	if err := dec.Decode(pubKey); err != nil {
		return fmt.Errorf("dkgState: failed to decode public key from %s: %v", msg.Addr, err)
	}
	m.pubKeys.Add(&PK2Addr{PK: pubKey, Addr: crypto.Address(msg.Addr)})

	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) SendDeals() (err error, ready bool) {
	if !m.IsReady() {
		return nil, false
	}

	messages, err := m.GetDeals()
	for _, msg := range messages {
		m.SendMsgCb(msg)
	}

	m.logger.Info("dkgState: sending deals", "deals", len(messages))

	return err, true
}

func (m *DKGDealer) IsReady() bool {
	return len(m.pubKeys) == m.validators.Size()
}

func (m *DKGDealer) GetDeals() ([]*types.DKGData, error) {
	sort.Sort(m.pubKeys)
	dkgInstance, err := dkg.NewDistKeyGenerator(m.suiteG2, m.secKey, m.pubKeys.GetPKs(), (m.validators.Size()*2)/3)
	if err != nil {
		return nil, fmt.Errorf("failed to create dkgState instance: %v", err)
	}
	m.instance = dkgInstance

	deals, err := m.instance.Deals()
	if err != nil {
		return nil, fmt.Errorf("failed to populate deals: %v", err)
	}
	for _, deal := range deals {
		m.participantID = int(deal.Index) // Same for each deal.
		break
	}

	var dealMessages []*types.DKGData
	for toIndex, deal := range deals {
		var (
			buf = bytes.NewBuffer(nil)
			enc = gob.NewEncoder(buf)
		)

		if err := enc.Encode(deal); err != nil {
			return dealMessages, fmt.Errorf("failed to encode deal #%d: %v", deal.Index, err)
		}

		dealMessage := &types.DKGData{
			Type:    types.DKGDeal,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
			Data:    buf.Bytes(),
			ToIndex: toIndex,
		}

		dealMessages = append(dealMessages, dealMessage)
	}

	return dealMessages, nil
}

func (m *DKGDealer) HandleDKGDeal(msg *types.DKGData) error {
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
	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessDeals() (err error, ready bool) {
	if !m.IsDealsReady() {
		return nil, false
	}
	m.logger.Info("dkgState: processing deals")

	responseMessages, err := m.GetResponses()
	for _, responseMsg := range responseMessages {
		m.SendMsgCb(responseMsg)
	}

	return err, true
}

func (m *DKGDealer) IsDealsReady() bool {
	return len(m.deals) >= m.validators.Size()-1
}

func (m *DKGDealer) GetResponses() ([]*types.DKGData, error) {
	var messages []*types.DKGData

	for _, deal := range m.deals {
		resp, err := m.instance.ProcessDeal(deal)
		if err != nil {
			return messages, fmt.Errorf("failed to ProcessDeal: %v", err)
		}
		var (
			buf = bytes.NewBuffer(nil)
			enc = gob.NewEncoder(buf)
		)
		if err := enc.Encode(resp); err != nil {
			return messages, fmt.Errorf("failed to encode response: %v", err)
		}

		messages = append(messages, &types.DKGData{
			Type:    types.DKGResponse,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
			Data:    buf.Bytes(),
		})
	}

	return messages, nil
}

func (m *DKGDealer) HandleDKGResponse(msg *types.DKGData) error {
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
	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessResponses() (err error, ready bool) {
	if !m.IsResponsesReady() {
		return nil, false
	}
	m.logger.Info("dkgState: processing responses")

	messages, err := m.GetJustifications()
	for _, msg := range messages {
		m.SendMsgCb(msg)
	}

	return err, true
}

func (m *DKGDealer) IsResponsesReady() bool {
	return len(m.responses) >= (m.validators.Size()-1)*(m.validators.Size()-1)
}

func (m *DKGDealer) processResponse(resp *dkg.Response) ([]byte, error) {
	if resp.Response.Approved {
		m.logger.Info("dkgState: deal is approved", "to", resp.Index, "from", resp.Response.Index)
	}

	justification, err := m.instance.ProcessResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to ProcessResponse: %v", err)
	}
	if justification == nil {
		return nil, nil
	}

	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err := enc.Encode(justification); err != nil {
		return nil, fmt.Errorf("failed to encode response: %v", err)
	}

	return buf.Bytes(), nil
}

func (m *DKGDealer) GetJustifications() ([]*types.DKGData, error) {
	var messages []*types.DKGData

	for _, resp := range m.responses {
		var msg = &types.DKGData{
			Type:    types.DKGJustification,
			RoundID: m.roundID,
			Addr:    m.addrBytes,
		}

		// In this call we might or might not put a justification to msg.Data.
		justificationBytes, err := m.processResponse(resp)
		if err != nil {
			return messages, err
		}

		msg.Data = justificationBytes
	}

	return messages, nil
}

func (m *DKGDealer) HandleDKGJustification(msg *types.DKGData) error {
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

	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessJustifications() (err error, ready bool) {
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
	m.SendMsgCb(&types.DKGData{
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

func (m *DKGDealer) HandleDKGCommit(msg *types.DKGData) error {
	dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
	commits := &dkg.SecretCommits{}
	for i := 0; i < msg.NumEntities; i++ {
		commits.Commitments = append(commits.Commitments, m.suiteG2.Point())
	}
	if err := dec.Decode(commits); err != nil {
		return fmt.Errorf("failed to decode commit: %v", err)
	}
	m.commits = append(m.commits, commits)

	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessCommits() (err error, ready bool) {
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
			m.SendMsgCb(msg)
		}
	}

	return nil, true
}

func (m *DKGDealer) HandleDKGComplaint(msg *types.DKGData) error {
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

	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessComplaints() (err error, ready bool) {
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
		m.SendMsgCb(msg)
	}

	return nil, true
}

func (m *DKGDealer) HandleDKGReconstructCommit(msg *types.DKGData) error {
	var rc *dkg.ReconstructCommits
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		rc = &dkg.ReconstructCommits{}
		if err := dec.Decode(rc); err != nil {
			return fmt.Errorf("failed to decode complaint: %v", err)
		}
	}

	m.reconstructCommits = append(m.reconstructCommits, rc)

	if err := m.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (m *DKGDealer) ProcessReconstructCommits() (err error, ready bool) {
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

func (m *DKGDealer) GetVerifier() (types.Verifier, error) {
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

func (m *DKGDealer) SendMsgCb(msg *types.DKGData) {
	m.sendMsgCb(msg)
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

type Dealer interface {
	Start() error
	Transit() error
	ResetDKGData()
	GenerateTransitions()
	GetLosers() []*types.Validator
	HandleDKGPubKey(msg *types.DKGData) error
	SetTransitions(t []transition)
	SendDeals() (err error, ready bool)
	IsReady() bool
	GetDeals() ([]*types.DKGData, error)
	HandleDKGDeal(msg *types.DKGData) error
	ProcessDeals() (err error, ready bool)
	IsDealsReady() bool
	GetResponses() ([]*types.DKGData, error)
	HandleDKGResponse(msg *types.DKGData) error
	ProcessResponses() (err error, ready bool)
	HandleDKGJustification(msg *types.DKGData) error
	ProcessJustifications() (err error, ready bool)
	IsResponsesReady() bool
	GetJustifications() ([]*types.DKGData, error)
	HandleDKGCommit(msg *types.DKGData) error
	ProcessCommits() (err error, ready bool)
	HandleDKGComplaint(msg *types.DKGData) error
	ProcessComplaints() (err error, ready bool)
	HandleDKGReconstructCommit(msg *types.DKGData) error
	ProcessReconstructCommits() (err error, ready bool)
	GetVerifier() (types.Verifier, error)
	SendMsgCb(*types.DKGData)
}
