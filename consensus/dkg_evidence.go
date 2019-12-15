package consensus

import (
	"bytes"
	"fmt"

	dkgAlias "github.com/dgamingfoundation/dkglib/lib/alias"

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
	pubkey crypto.PubKey
	height int64
}

// String returns a string representation of the evidence.
func (m *DKGEvidenceMissingData) String() string {
	return fmt.Sprintf("Anndress: %s", m.pubkey.Address().String())

}

// Height returns the height this evidence refers to.
func (m *DKGEvidenceMissingData) Height() int64 {
	return m.height
}

// Address returns the address of the validator.
func (m *DKGEvidenceMissingData) Address() []byte {
	return m.pubkey.Address()
}

// Hash returns the hash of the evidence.
func (m *DKGEvidenceMissingData) Bytes() []byte {
	return cdc.MustMarshalBinaryBare(m)
}

// Hash returns the hash of the evidence.
func (m *DKGEvidenceMissingData) Hash() []byte {
	return tmhash.Sum(cdc.MustMarshalBinaryBare(m))
}

// Verify returns an error if the two votes aren't conflicting.
// To be conflicting, they must be from the same validator, for the same H/R/S, but for different blocks.
func (m *DKGEvidenceMissingData) Verify(chainID string, pubKey crypto.PubKey) error {
	return nil
}

// Equal checks if two pieces of evidence are equal.
func (m *DKGEvidenceMissingData) Equal(ev types.Evidence) bool {
	if _, ok := ev.(*DKGEvidenceMissingData); !ok {
		return false
	}

	// just check their hashes
	mHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	evHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	return bytes.Equal(mHash, evHash)
}

// ValidateBasic performs basic validation.
func (m *DKGEvidenceMissingData) ValidateBasic() error {
	if len(m.pubkey.Bytes()) == 0 {
		return errors.New("Empty PubKey")
	}

	return nil
}

// ---------------------------------------------------------------------------
//
//  DKGEvidenceCorruptData
//
// ---------------------------------------------------------------------------

// DKGEvidenceCorruptData should contain evidence about corrupt data that was actually
// sent by a node.
type DKGEvidenceCorruptData struct {
	data            *dkgAlias.DKGData
	validatorPubKey crypto.PubKey
	height          int64
}

// String returns a string representation of the evidence.
func (m *DKGEvidenceCorruptData) String() string {
	return fmt.Sprintf("Anndress: %s", m.validatorPubKey.Address().String())

}

// Height returns the height this evidence refers to.
func (m *DKGEvidenceCorruptData) Height() int64 {
	return m.height
}

// Address returns the address of the validator.
func (m *DKGEvidenceCorruptData) Address() []byte {
	return m.validatorPubKey.Address()
}

// Hash returns the bytes which compromise the evidence
func (m *DKGEvidenceCorruptData) Bytes() []byte {
	return cdc.MustMarshalBinaryBare(m)
}

// Hash returns the hash of the evidence.
func (m *DKGEvidenceCorruptData) Hash() []byte {
	return tmhash.Sum(cdc.MustMarshalBinaryBare(m))
}

// Verify the evidence.
func (m *DKGEvidenceCorruptData) Verify(chainID string, pubKey crypto.PubKey) error {
	switch m.data.Type {
	case dkgAlias.DKGPubKey:
		// pass
	case dkgAlias.DKGDeal:
		// pass
	case dkgAlias.DKGResponse:
		// pass
	case dkgAlias.DKGJustification:
		// pass
	case dkgAlias.DKGCommits:
		// pass
	case dkgAlias.DKGComplaint:
		// pass
	case dkgAlias.DKGReconstructCommit:
		// pass
	}

	return nil
}

// Equal checks if two pieces of evidence are equal.
func (m *DKGEvidenceCorruptData) Equal(ev types.Evidence) bool {
	if _, ok := ev.(*DKGEvidenceCorruptData); !ok {
		return false
	}

	// just check their hashes
	mHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	evHash := tmhash.Sum(cdc.MustMarshalBinaryBare(m))
	return bytes.Equal(mHash, evHash)
}

// ValidateBasic performs basic validation.
func (m *DKGEvidenceCorruptData) ValidateBasic() error {
	if len(m.validatorPubKey.Bytes()) == 0 {
		return errors.New("Empty PubKey")
	}

	return nil
}
