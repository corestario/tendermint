package alias

import (
	"github.com/tendermint/tendermint/types"
)

var (
	NewValidatorSet     = types.NewValidatorSet
	RegisterBlockAmino  = types.RegisterBlockAmino
	NewMockPVWithParams = types.NewMockPVWithParams
)

type (
	Validator     = types.Validator
	ValidatorSet  = types.ValidatorSet
	PrivValidator = types.PrivValidator
	Vote          = types.Vote
	MockPV        = types.MockPV
)

var (
	MsgQueueSize = 1000
)
