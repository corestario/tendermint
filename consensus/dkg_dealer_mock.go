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

type DKGMock struct {
	Dealer
}

func NewDKGMockDealerNoDeal(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	fmt.Println("++++++++++++++++++ 3")
	return &DKGMock{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMock) SendDeals() (err error, ready bool) {
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

func (m *DKGMock) GetDeals() ([]*types.DKGData, error) {
	fmt.Println("+++++++++++++++ 2")
	deals, err := m.Dealer.GetDeals()

	// remove one deal message
	deals = deals[:len(deals)-1]
	return nil, err
}
