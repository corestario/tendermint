package consensus

import (
	"fmt"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type DKGMockDontSendOneJustification struct {
	Dealer
}

func NewDKGMockDealerNoJustification(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	return &DKGMockDontSendOneJustification{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMockDontSendOneJustification) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendOneJustification) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.Dealer.SendDeals,
		m.Dealer.ProcessDeals,
		m.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendOneJustification) ProcessResponses() (error, bool) {
	if !m.Dealer.IsResponsesReady() {
		return nil, false
	}

	messages, err := m.GetJustifications()
	if err != nil {
		return err, true
	}
	for _, msg := range messages {
		m.Dealer.SendMsgCb(msg)
	}

	fmt.Println("dkgState: sending justifications", "justifications", len(messages))

	return nil, true
}

func (m *DKGMockDontSendOneJustification) GetResponses() ([]*types.DKGData, error) {
	responses, err := m.Dealer.GetResponses()

	// remove one response message
	responses = responses[:len(responses)-1]

	return responses, err
}

type DKGMockDontSendAnyJustifications struct {
	Dealer
}

func NewDKGMockDealerAnyJustifications(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	return &DKGMockDontSendAnyJustifications{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMockDontSendAnyJustifications) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendAnyJustifications) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.Dealer.SendDeals,
		m.Dealer.ProcessDeals,
		m.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendAnyJustifications) ProcessResponses() (error, bool) {
	if !m.Dealer.IsResponsesReady() {
		return nil, false
	}

	fmt.Println("dkgState: sending justifications", "justifications", 0)

	return nil, true
}
