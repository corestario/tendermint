package consensus

import (
	"fmt"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type DKGMock struct {
	Dealer
}

func NewDKGMockDealer(validators *types.ValidatorSet, pubKey crypto.PubKey, sendMsgCb func(*types.DKGData), logger log.Logger) Dealer {
	return &DKGMock{NewDKGDealer(validators, pubKey, sendMsgCb, logger)}
}

func (m *DKGMock) HandleDKGPubKey(msg *types.DKGData) error {
	fmt.Println("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	return m.Dealer.HandleDKGPubKey(msg)
}
