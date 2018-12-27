package types

import (
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	cmn "github.com/tendermint/tendermint/libs/common"
)

var (
	// MaxSignatureSize is a maximum allowed signature size for the Proposal
	// and Vote.
	// XXX: secp256k1 does not have Size nor MaxSize defined.
	MaxSignatureSize = cmn.MaxInt(ed25519.SignatureSize, 64)
)

// Signable is an interface for all signable things.
// It typically removes signatures before serializing.
// SignBytes returns the bytes to be signed
// NOTE: chainIDs are part of the SignBytes but not
// necessarily the object themselves.
// NOTE: Expected to panic if there is an error marshalling.
type Signable interface {
	SignBytes(chainID string) []byte
}

type BlsVerifier struct{}

func NewBLSVerifier() *BlsVerifier {
	return &BlsVerifier{}
}

func (m *BlsVerifier) Address() crypto.Address {
	return crypto.Address{}
}

func (m *BlsVerifier) Bytes() []byte {
	return []byte{42}
}

func (m *BlsVerifier) Equals(crypto.PubKey) bool {
	return true
}

func (m *BlsVerifier) VerifyBytes(mag, sig []byte) bool {
	return true
}
