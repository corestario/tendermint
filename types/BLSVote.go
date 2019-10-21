package types

import (
	"fmt"
	cmn "github.com/tendermint/tendermint/libs/common"
)

type BLSVote struct {
	Vote
	BLSSignature []byte `json:"bls_signature"`
}

func (vote *BLSVote) GetBLSSignature() []byte {
	return vote.BLSSignature
}

func (vote *BLSVote) String() string {
	if vote == nil {
		return nilVoteStr
	}
	var typeString string
	switch vote.Type {
	case PrevoteType:
		typeString = "Prevote"
	case PrecommitType:
		typeString = "Precommit"
	default:
		panic("Unknown vote type")
	}

	return fmt.Sprintf("Vote{%v:%X %v/%02d/%v(%v) %X %X @ %s BLSSignature: %+v}",
		vote.ValidatorIndex,
		cmn.Fingerprint(vote.ValidatorAddress),
		vote.Height,
		vote.Round,
		vote.Type,
		typeString,
		cmn.Fingerprint(vote.BlockID.Hash),
		cmn.Fingerprint(vote.Signature),
		CanonicalTime(vote.Timestamp),
		vote.BLSSignature,
	)
}
