package consensus

import (
	"fmt"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

func NewDealerConstructor(indexToConstructor map[int]DKGDealerConstructor) func(i int) DKGDealerConstructor {
	return func(i int) DKGDealerConstructor {
		if constructor, ok := indexToConstructor[i]; ok {
			return constructor
		}
		return NewDKGDealer
	}
}

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

func (m *DKGMockDontSendOneDeal) SendDeals() (err error, ready bool) {
	fmt.Println("+++++++++++++++ 1")
	if !m.Dealer.IsReady() {
		return nil, false
	}

	dealMessages, err := m.GetDeals()
	for _, dealMsg := range dealMessages {
		m.Dealer.SendMsgCb(dealMsg)
	}

	fmt.Println("dkgState: sending deals", "deals", len(dealMessages))

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

func (m *DKGMockDontSendAnyDeal) SendDeals() (err error, ready bool) {
	if !m.Dealer.IsReady() {
		return nil, false
	}

	return nil, true
}
