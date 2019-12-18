package state

import (
	"bytes"
	"fmt"

	dkgTypes "github.com/dgamingfoundation/dkglib/lib/types"

	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/tmhash"
	"github.com/tendermint/tendermint/types"
)

// ---------------------------------------------------------------------------
//
// DKGEvidenceMissingData
//
// ---------------------------------------------------------------------------

// DKGEvidenceMissingData should contain evidence about a node that was expected
// to send some data but failed to do so.
type DKGEvidenceMissingData struct {
	DKGEvidence
}

// ---------------------------------------------------------------------------
//
//  DKGEvidenceCorruptData
//
// ---------------------------------------------------------------------------

// DKGEvidenceCorruptData should contain evidence about corrupt data that was actually
// sent by a node.
type DKGEvidenceCorruptData struct {
	DKGEvidence
}

// ---------------------------------------------------------------------------
//
//  DKGEvidenceCorruptJustification
//
// ---------------------------------------------------------------------------

// DKGEvidenceCorruptJustification should contain evidence about corrupt data that was actually
// sent by a node.
type DKGEvidenceCorruptJustification struct {
	DKGEvidence
}

// ---------------------------------------------------------------------------
//
//  DKGEvidence (base type)
//
// ---------------------------------------------------------------------------

type DKGEvidence struct {
	Loser           *dkgTypes.DKGLoser
	ValidatorPubKey crypto.PubKey
	height          int64
}

// String returns a string representation of the evidence.
func (m *DKGEvidence) String() string {
	return fmt.Sprintf("Anndress: %s", m.ValidatorPubKey.Address().String())

}

// Height returns the height this evidence refers to.
func (m *DKGEvidence) Height() int64 {
	return m.height
}

// Address returns the address of the validator.
func (m *DKGEvidence) Address() []byte {
	return m.ValidatorPubKey.Address()
}

// Hash returns the hash of the evidence.
func (m *DKGEvidence) Bytes() []byte {
	return cdc.MustMarshalBinaryBare(m)
}

// Hash returns the hash of the evidence.
func (m *DKGEvidence) Hash() []byte {
	return tmhash.Sum(cdc.MustMarshalBinaryBare(m))
}

// Verify returns an error if the two votes aren't conflicting.
// To be conflicting, they must be from the same validator, for the same H/R/S, but for different blocks.
func (m *DKGEvidence) Verify(chainID string, pubKey crypto.PubKey) error {
	return nil
}

// Equal checks if two pieces of evidence are equal.
func (m *DKGEvidence) Equal(ev types.Evidence) bool {
	if _, ok := ev.(*DKGEvidenceMissingData); !ok {
		return false
	}

	// just check their hashes
	mHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	evHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	return bytes.Equal(mHash, evHash)
}

// ValidateBasic performs basic validation.
func (m *DKGEvidence) ValidateBasic() error {
	if len(m.ValidatorPubKey.Bytes()) == 0 {
		return errors.New("Empty PubKey")
	}

	return nil
}
