package consensus

import (
	"fmt"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type DKGMockDontSendOneDeal struct {
	Dealer
}

func NewDKGMockDealerNoDeal(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	return &DKGMockDontSendOneDeal{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMockDontSendOneDeal) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendOneDeal) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.SendDeals,
		m.Dealer.ProcessDeals,
		m.Dealer.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendOneDeal) SendDeals() (error, bool) {
	fmt.Println("+++++++++++++++ 1")
	if !m.Dealer.IsReady() {
		return nil, false
	}

	messages, err := m.GetDeals()
	if err != nil {
		return err, true
	}
	for _, msg := range messages {
		m.Dealer.SendMsgCb(msg)
	}

	fmt.Println("dkgState: sending deals", "deals", len(messages))

	return nil, true
}

func (m *DKGMockDontSendOneDeal) GetDeals() ([]*types.DKGData, error) {
	deals, err := m.Dealer.GetDeals()

	// remove one deal message
	deals = deals[:len(deals)-1]

	return deals, err
}


type DKGMockDontSendAnyDeal struct {
	Dealer
}

func NewDKGMockDealerAnyDeal(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	return &DKGMockDontSendAnyDeal{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMockDontSendAnyDeal) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendAnyDeal) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.SendDeals,
		m.Dealer.ProcessDeals,
		m.Dealer.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendAnyDeal) SendDeals() (error, bool) {
	if !m.Dealer.IsReady() {
		return nil, false
	}

	fmt.Println("dkgState: sending deals", "deals", 0)

	return nil, true
}
