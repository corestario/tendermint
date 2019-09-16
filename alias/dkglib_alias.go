package alias

import (
	"github.com/dgamingfoundation/tendermint/types"
)

var (
	NewValidatorSet    = types.NewValidatorSet
	RegisterBlockAmino = types.RegisterBlockAmino
)

type (
	Validator     = types.Validator
	ValidatorSet  = types.ValidatorSet
	PrivValidator = types.PrivValidator
	Vote          = types.Vote
)
