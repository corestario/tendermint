package consensus

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"sort"

	"encoding/hex"
	"math"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"go.dedis.ch/kyber/v3"
	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/share"
	dkg "go.dedis.ch/kyber/v3/share/dkg/rabin"
	vss "go.dedis.ch/kyber/v3/share/vss/rabin"
)

type Dealer interface {
	Start() error
	GetState() DealerState
	Transit() error
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
	IsJustificationsReady() bool
	GetCommits() (*dkg.SecretCommits, error)
	HandleDKGComplaint(msg *types.DKGData) error
	ProcessComplaints() (err error, ready bool)
	HandleDKGReconstructCommit(msg *types.DKGData) error
	ProcessReconstructCommits() (err error, ready bool)
	GetVerifier() (types.Verifier, error)
	SendMsgCb(*types.DKGData) error
	VerifyMessage(msg DKGDataMessage) error
}

type DKGDealer struct {
	DealerState
	eventFirer events.Fireable

	sendMsgCb func(*types.DKGData) error
	logger    log.Logger

	pubKey      kyber.Point
	secKey      kyber.Scalar
	suiteG1     *bn256.Suite
	suiteG2     *bn256.Suite
	instance    *dkg.DistKeyGenerator
	transitions []transition

	pubKeys            PKStore
	deals              map[string]*dkg.Deal
	responses          []*dkg.Response
	justifications     []*dkg.Justification
	commits            []*dkg.SecretCommits
	complaints         []*dkg.ComplaintCommits
	reconstructCommits []*dkg.ReconstructCommits

	losers []crypto.Address
}

type DealerState struct {
	validators *types.ValidatorSet
	addrBytes  []byte

	participantID int
	roundID       int
}

func (ds DealerState) GetValidatorsCount() int {
	if ds.validators == nil {
		return 0
	}
	return ds.validators.Size()
}

func (ds DealerState) GetRoundID() int { return ds.roundID }

type DKGDealerConstructor func(validators *types.ValidatorSet, pv types.PrivValidator, sendMsgCb func(*types.DKGData) error, eventFirer events.Fireable, logger log.Logger, startRound int) Dealer

func NewDKGDealer(validators *types.ValidatorSet, pv types.PrivValidator, sendMsgCb func(*types.DKGData) error, eventFirer events.Fireable, logger log.Logger, startRound int) Dealer {
	return &DKGDealer{
		DealerState: DealerState{
			validators: validators,
			addrBytes:  pv.GetPubKey().Address().Bytes(),
			roundID:    startRound,
		},
		sendMsgCb:  sendMsgCb,
		eventFirer: eventFirer,
		logger:     logger,
		suiteG1:    bn256.NewSuiteG1(),
		suiteG2:    bn256.NewSuiteG2(),

		deals: make(map[string]*dkg.Deal),
	}
}

func (d *DKGDealer) Start() error {
	d.roundID++
	d.secKey = d.suiteG2.Scalar().Pick(d.suiteG2.RandomStream())
	d.pubKey = d.suiteG2.Point().Mul(d.secKey, nil)

	d.GenerateTransitions()

	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err := enc.Encode(d.pubKey); err != nil {
		return fmt.Errorf("failed to encode public key: %v", err)
	}

	d.logger.Info("dkgState: sending pub key", "key", d.pubKey.String())
	err := d.SendMsgCb(&types.DKGData{
		Type:    types.DKGPubKey,
		RoundID: d.roundID,
		Addr:    d.addrBytes,
		Data:    buf.Bytes(),
	})
	if err != nil {
		return fmt.Errorf("failed to sign message: %v", err)
	}

	return nil
}

func (d *DKGDealer) GetState() DealerState {
	return d.DealerState
}

func (d *DKGDealer) Transit() error {
	for len(d.transitions) > 0 {
		var tn = d.transitions[0]
		err, ready := tn()
		if !ready {
			return nil
		}
		if err != nil {
			return err
		}
		d.transitions = d.transitions[1:]
	}

	return nil
}

func (d *DKGDealer) GenerateTransitions() {
	d.transitions = []transition{
		// Phase I
		d.SendDeals,
		d.ProcessDeals,
		d.ProcessResponses,
		d.ProcessJustifications,
		// Phase II
		d.ProcessCommits,
		d.ProcessComplaints,
		d.ProcessReconstructCommits,
	}
}

func (d *DKGDealer) SetTransitions(t []transition) {
	d.transitions = t
}

func (d *DKGDealer) GetLosers() []*types.Validator {
	var out []*types.Validator
	for _, loser := range d.losers {
		_, validator := d.validators.GetByAddress(loser)
		out = append(out, validator)
	}

	return out
}

//////////////////////////////////////////////////////////////////////////////
//
// PHASE I
//
//////////////////////////////////////////////////////////////////////////////

func (d *DKGDealer) HandleDKGPubKey(msg *types.DKGData) error {
	var (
		dec    = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		pubKey = d.suiteG2.Point()
	)
	if err := dec.Decode(pubKey); err != nil {
		return fmt.Errorf("dkgState: failed to decode public key from %s: %v", msg.Addr, err)
	}
	d.pubKeys.Add(&PK2Addr{PK: pubKey, Addr: crypto.Address(msg.Addr)})

	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) SendDeals() (error, bool) {
	if !d.IsReady() {
		return nil, false
	}
	d.eventFirer.FireEvent(types.EventDKGPubKeyReceived, nil)

	messages, err := d.GetDeals()
	if err != nil {
		return fmt.Errorf("failed to get deals: %v", err), true
	}

	for _, msg := range messages {
		if err = d.SendMsgCb(msg); err != nil {
			return fmt.Errorf("failed to sign message: %v", err), true
		}
	}

	d.logger.Info("dkgState: sending deals", "deals", len(messages))

	return err, true
}

func (d *DKGDealer) IsReady() bool {
	return len(d.pubKeys) == d.validators.Size()
}

func (d *DKGDealer) GetDeals() ([]*types.DKGData, error) {
	// It's needed for DistKeyGenerator and for binary search in array
	sort.Sort(d.pubKeys)
	dkgInstance, err := dkg.NewDistKeyGenerator(d.suiteG2, d.secKey, d.pubKeys.GetPKs(), (d.validators.Size()*2)/3)
	if err != nil {
		return nil, fmt.Errorf("failed to create dkgState instance: %v", err)
	}
	d.instance = dkgInstance

	// We have N - 1 deals produced here (here and below N stands for the number of validators).
	deals, err := d.instance.Deals()
	if err != nil {
		return nil, fmt.Errorf("failed to populate deals: %v", err)
	}
	for _, deal := range deals {
		d.participantID = int(deal.Index) // Same for each deal.
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
			RoundID: d.roundID,
			Addr:    d.addrBytes,
			Data:    buf.Bytes(),
			ToIndex: toIndex,
		}

		dealMessages = append(dealMessages, dealMessage)
	}

	return dealMessages, nil
}

func (d *DKGDealer) HandleDKGDeal(msg *types.DKGData) error {
	var (
		dec  = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		deal = &dkg.Deal{ // We need to initialize everything down to the kyber.Point to avoid nil panics.
			Deal: &vss.EncryptedDeal{
				DHKey: d.suiteG2.Point(),
			},
		}
	)
	if err := dec.Decode(deal); err != nil {
		return fmt.Errorf("failed to decode deal: %v", err)
	}

	// We expect to keep N - 1 deals (we don't care about the deals sent to other participants).
	if d.participantID != msg.ToIndex {
		d.logger.Debug("dkgState: rejecting deal (intended for another participant)", "intended", msg.ToIndex)
		return nil
	}

	d.logger.Info("dkgState: deal is intended for us, storing")
	if _, exists := d.deals[msg.GetAddrString()]; exists {
		return nil
	}

	d.deals[msg.GetAddrString()] = deal
	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessDeals() (error, bool) {
	if !d.IsDealsReady() {
		return nil, false
	}

	d.logger.Info("dkgState: processing deals")
	responseMessages, err := d.GetResponses()
	if err != nil {
		return fmt.Errorf("failed to get responses: %v", err), true
	}

	for _, responseMsg := range responseMessages {
		if err = d.SendMsgCb(responseMsg); err != nil {
			return fmt.Errorf("failed to sign message: %v", err), true
		}

	}

	return err, true
}

func (d *DKGDealer) IsDealsReady() bool {
	return len(d.deals) >= d.validators.Size()-1
}

func (d *DKGDealer) GetResponses() ([]*types.DKGData, error) {
	var messages []*types.DKGData

	// Each deal produces a response for the deal's issuer (that makes N - 1 responses).
	for _, deal := range d.deals {
		resp, err := d.instance.ProcessDeal(deal)
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
			RoundID: d.roundID,
			Addr:    d.addrBytes,
			Data:    buf.Bytes(),
		})
	}
	d.eventFirer.FireEvent(types.EventDKGDealsProcessed, d.roundID)

	return messages, nil
}

func (d *DKGDealer) HandleDKGResponse(msg *types.DKGData) error {
	var (
		dec  = gob.NewDecoder(bytes.NewBuffer(msg.Data))
		resp = &dkg.Response{}
	)
	if err := dec.Decode(resp); err != nil {
		return fmt.Errorf("failed to decode deal: %v", err)
	}

	// Unlike the procedure for deals, with responses we do care about other
	// participants state of affairs. All responses sent make N * (N - 1) responses,
	// but we skip the responses produced by  ourselves, which gives
	// N * (N - 1) - (N - 1) responses, which gives (N - 1) ^ 2 responses.
	if uint32(d.participantID) == resp.Response.Index {
		d.logger.Debug("dkgState: skipping response")
		return nil
	}

	d.logger.Info("dkgState: response is intended for us, storing")

	d.responses = append(d.responses, resp)
	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessResponses() (error, bool) {
	if !d.IsResponsesReady() {
		return nil, false
	}

	messages, err := d.GetJustifications()
	if err != nil {
		return fmt.Errorf("failed to get justifications: %v", err), true
	}

	for _, msg := range messages {
		if err = d.SendMsgCb(msg); err != nil {
			return fmt.Errorf("failed to sign message: %v", err), true
		}
	}

	return err, true
}

func (d *DKGDealer) IsResponsesReady() bool {
	return len(d.responses) >= int(math.Pow(float64(d.validators.Size()-1), 2))
}

func (d *DKGDealer) processResponse(resp *dkg.Response) ([]byte, error) {
	if resp.Response.Approved {
		d.logger.Info("dkgState: deal is approved", "to", resp.Index, "from", resp.Response.Index)
	}

	justification, err := d.instance.ProcessResponse(resp)
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

func (d *DKGDealer) GetJustifications() ([]*types.DKGData, error) {
	var messages []*types.DKGData

	for _, resp := range d.responses {
		var msg = &types.DKGData{
			Type:    types.DKGJustification,
			RoundID: d.roundID,
			Addr:    d.addrBytes,
		}

		// Each of (N - 1) ^ 2 received response generates a (possibly nil) justification.
		// Nil justifications (and other nil messages) are used to avoid having timeouts
		// (i.e., this allows us to know exactly how many messages should be received to
		// proceed). This might be changed in the future.
		justificationBytes, err := d.processResponse(resp)
		if err != nil {
			return messages, err
		}

		msg.Data = justificationBytes
		// We will nave N * (N - 1) ^ 2 justifications. This looks rather bad, actually
		messages = append(messages, msg)
	}

	d.eventFirer.FireEvent(types.EventDKGResponsesProcessed, d.roundID)
	return messages, nil
}

func (d *DKGDealer) HandleDKGJustification(msg *types.DKGData) error {
	var justification *dkg.Justification
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		justification = &dkg.Justification{}
		if err := dec.Decode(justification); err != nil {
			return fmt.Errorf("failed to decode deal: %v", err)
		}
	}

	d.justifications = append(d.justifications, justification)

	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessJustifications() (error, bool) {
	if !d.IsJustificationsReady() {
		return nil, false
	}
	d.logger.Info("dkgState: processing justifications")

	commits, err := d.GetCommits()
	if err != nil {
		return err, true
	}

	var (
		buf = bytes.NewBuffer(nil)
		enc = gob.NewEncoder(buf)
	)
	if err = enc.Encode(commits); err != nil {
		return fmt.Errorf("failed to encode response: %v", err), true
	}

	message := &types.DKGData{
		Type:        types.DKGCommits,
		RoundID:     d.roundID,
		Addr:        d.addrBytes,
		Data:        buf.Bytes(),
		NumEntities: len(commits.Commitments),
	}

	err = d.SendMsgCb(message)
	if err != nil {
		return fmt.Errorf("failed to sign message: %v", err), true
	}

	return nil, true
}

func (d *DKGDealer) IsJustificationsReady() bool {
	// N * (N - 1) ^ 2.
	return len(d.justifications) >= d.validators.Size()*int(math.Pow(float64(d.validators.Size()-1), 2))
}

func (d DKGDealer) GetCommits() (*dkg.SecretCommits, error) {
	for _, justification := range d.justifications {
		if justification != nil {
			d.logger.Info("dkgState: processing non-empty justification", "from", justification.Index)
			if err := d.instance.ProcessJustification(justification); err != nil {
				return nil, fmt.Errorf("failed to ProcessJustification: %v", err)
			}
		} else {
			d.logger.Info("dkgState: empty justification, everything is o.k.")
		}
	}
	d.eventFirer.FireEvent(types.EventDKGJustificationsProcessed, d.roundID)

	if !d.instance.Certified() {
		return nil, errors.New("instance is not certified")
	}
	d.eventFirer.FireEvent(types.EventDKGInstanceCertified, d.roundID)

	qual := d.instance.QUAL()
	d.logger.Info("dkgState: got the QUAL set", "qual", qual)
	if len(qual) < d.validators.Size() {
		qualSet := map[int]bool{}
		for _, idx := range qual {
			qualSet[idx] = true
		}

		for idx, pk2addr := range d.pubKeys {
			if !qualSet[idx] {
				d.losers = append(d.losers, pk2addr.Addr)
			}
		}

		return nil, errors.New("some of participants failed to complete phase I")
	}

	commits, err := d.instance.SecretCommits()
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %v", err)
	}

	return commits, nil
}

//////////////////////////////////////////////////////////////////////////////
//
// PHASE II
//
//////////////////////////////////////////////////////////////////////////////

func (d *DKGDealer) HandleDKGCommit(msg *types.DKGData) error {
	dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
	commits := &dkg.SecretCommits{}
	for i := 0; i < msg.NumEntities; i++ {
		commits.Commitments = append(commits.Commitments, d.suiteG2.Point())
	}
	if err := dec.Decode(commits); err != nil {
		return fmt.Errorf("failed to decode commit: %v", err)
	}
	d.commits = append(d.commits, commits)

	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessCommits() (error, bool) {
	if len(d.commits) < len(d.instance.QUAL()) {
		return nil, false
	}
	d.logger.Info("dkgState: processing commits")

	var alreadyFinished = true
	var messages []*types.DKGData
	for _, commits := range d.commits {
		var msg = &types.DKGData{
			Type:    types.DKGComplaint,
			RoundID: d.roundID,
			Addr:    d.addrBytes,
		}
		complaint, err := d.instance.ProcessSecretCommits(commits)
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
	d.eventFirer.FireEvent(types.EventDKGCommitsProcessed, d.roundID)

	if !alreadyFinished {
		for _, msg := range messages {
			if err := d.SendMsgCb(msg); err != nil {
				return fmt.Errorf("failed to sign message: %v", err), true
			}

		}
	}

	return nil, true
}

func (d *DKGDealer) HandleDKGComplaint(msg *types.DKGData) error {
	var complaint *dkg.ComplaintCommits
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		complaint = &dkg.ComplaintCommits{
			Deal: &vss.Deal{},
		}
		for i := 0; i < msg.NumEntities; i++ {
			complaint.Deal.Commitments = append(complaint.Deal.Commitments, d.suiteG2.Point())
		}
		if err := dec.Decode(complaint); err != nil {
			return fmt.Errorf("failed to decode complaint: %v", err)
		}
	}

	d.complaints = append(d.complaints, complaint)

	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessComplaints() (error, bool) {
	if len(d.complaints) < len(d.instance.QUAL())-1 {
		return nil, false
	}
	d.logger.Info("dkgState: processing commits")

	for _, complaint := range d.complaints {
		var msg = &types.DKGData{
			Type:    types.DKGReconstructCommit,
			RoundID: d.roundID,
			Addr:    d.addrBytes,
		}
		if complaint != nil {
			reconstructionMsg, err := d.instance.ProcessComplaintCommits(complaint)
			if err != nil {
				return fmt.Errorf("failed to ProcessComplaintCommits: %v", err), true
			}
			if reconstructionMsg != nil {
				var (
					buf = bytes.NewBuffer(nil)
					enc = gob.NewEncoder(buf)
				)
				if err = enc.Encode(complaint); err != nil {
					return fmt.Errorf("failed to encode response: %v", err), true
				}
				msg.Data = buf.Bytes()
			}
		}

		if err := d.SendMsgCb(msg); err != nil {
			return fmt.Errorf("failed to sign message: %v", err), true
		}

	}
	d.eventFirer.FireEvent(types.EventDKGComplaintProcessed, d.roundID)
	return nil, true
}

func (d *DKGDealer) HandleDKGReconstructCommit(msg *types.DKGData) error {
	var rc *dkg.ReconstructCommits
	if msg.Data != nil {
		dec := gob.NewDecoder(bytes.NewBuffer(msg.Data))
		rc = &dkg.ReconstructCommits{}
		if err := dec.Decode(rc); err != nil {
			return fmt.Errorf("failed to decode complaint: %v", err)
		}
	}

	d.reconstructCommits = append(d.reconstructCommits, rc)

	if err := d.Transit(); err != nil {
		return fmt.Errorf("failed to Transit: %v", err)
	}

	return nil
}

func (d *DKGDealer) ProcessReconstructCommits() (error, bool) {
	if len(d.reconstructCommits) < len(d.instance.QUAL())-1 {
		return nil, false
	}

	for _, rc := range d.reconstructCommits {
		if rc == nil {
			continue
		}
		if err := d.instance.ProcessReconstructCommits(rc); err != nil {
			return fmt.Errorf("failed to ProcessReconstructCommits: %v", err), true
		}
	}
	d.eventFirer.FireEvent(types.EventDKGReconstructCommitsProcessed, d.roundID)

	if !d.instance.Finished() {
		return errors.New("dkgState round is finished, but dkgState instance is not ready"), true
	}

	return nil, true
}

func (d *DKGDealer) GetVerifier() (types.Verifier, error) {
	if d.instance == nil || !d.instance.Finished() {
		return nil, ErrDKGVerifierNotReady
	}

	distKeyShare, err := d.instance.DistKeyShare()
	if err != nil {
		return nil, fmt.Errorf("failed to get DistKeyShare: %v", err)
	}

	var (
		masterPubKey = share.NewPubPoly(bn256.NewSuiteG2(), nil, distKeyShare.Commitments())
		newShare     = &types.BLSShare{
			ID:   d.participantID,
			Pub:  &share.PubShare{I: d.participantID, V: d.pubKey},
			Priv: distKeyShare.PriShare(),
		}
		t, n = (d.validators.Size() / 3) * 2, d.validators.Size()
	)

	return types.NewBLSVerifier(masterPubKey, newShare, t, n), nil
}

// VerifyMessage verify message by signature
func (d *DKGDealer) VerifyMessage(msg DKGDataMessage) error {
	var (
		signBytes []byte
		err       error
	)
	_, validator := d.validators.GetByAddress(msg.Data.Addr)
	if validator == nil {
		return fmt.Errorf("can't find validator by address: %s", msg.Data.GetAddrString())
	}
	if signBytes, err = msg.Data.SignBytes(); err != nil {
		return err
	}
	if !validator.PubKey.VerifyBytes(signBytes, msg.Data.Signature) {
		return fmt.Errorf("invalid DKG message signature: %s", hex.EncodeToString(msg.Data.Signature))
	}
	return nil
}

func (d *DKGDealer) SendMsgCb(msg *types.DKGData) error {
	return d.sendMsgCb(msg)
}

type PK2Addr struct {
	Addr crypto.Address
	PK   kyber.Point
}

type PKStore []*PK2Addr

func (s *PKStore) Add(newPk *PK2Addr) bool {
	for _, pk := range *s {
		if pk.Addr.String() == newPk.Addr.String() && pk.PK.Equal(newPk.PK) {
			return false
		}
	}
	*s = append(*s, newPk)

	return true
}

func (s PKStore) Len() int           { return len(s) }
func (s PKStore) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s PKStore) Less(i, j int) bool { return s[i].Addr.String() < s[j].Addr.String() }
func (s PKStore) GetPKs() []kyber.Point {
	var out = make([]kyber.Point, len(s))
	for idx, val := range s {
		out[idx] = val.PK
	}
	return out
}

type transition func() (error, bool)

type Justification struct {
	Void          bool
	Justification *dkg.Justification
}
