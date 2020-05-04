package node

import (
	amino "github.com/tendermint/go-amino"

	cryptoAmino "github.com/tendermint/tendermint/crypto/encoding/amino"
)

var Cdc = amino.NewCodec()

func init() {
	cryptoAmino.RegisterAmino(Cdc)
}
