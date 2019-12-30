package proxy

import (
	"github.com/tendermint/tendermint/crypto/merkle"
)

func DefaultProofRuntime() *merkle.ProofRuntime {
	prt := merkle.NewProofRuntime()
	prt.RegisterOpDecoder(
		merkle.ProofOpSimpleValue,
		merkle.SimpleValueOpDecoder,
	)
	return prt
}
