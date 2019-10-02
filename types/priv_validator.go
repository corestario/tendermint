package types

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

type DataSigner interface {
	SignBytes(string) []byte
	SetSignature([]byte)
}

// PrivValidator defines the functionality of a local Tendermint validator
// that signs votes and proposals, and never double signs.
type PrivValidator interface {
	// TODO: Extend the interface to return errors too. Issue: https://github.com/tendermint/tendermint/issues/3602
	GetPubKey() crypto.PubKey

	SignVote(chainID string, vote *Vote) error
	SignProposal(chainID string, proposal *Proposal) error

	SignData(chainID string, data DataSigner) error
}

//----------------------------------------
// Misc.

type PrivValidatorsByAddress []PrivValidator

func (pvs PrivValidatorsByAddress) Len() int {
	return len(pvs)
}

func (pvs PrivValidatorsByAddress) Less(i, j int) bool {
	return bytes.Compare(pvs[i].GetPubKey().Address(), pvs[j].GetPubKey().Address()) == -1
}

func (pvs PrivValidatorsByAddress) Swap(i, j int) {
	it := pvs[i]
	pvs[i] = pvs[j]
	pvs[j] = it
}

//----------------------------------------
// MockPV

// MockPV implements PrivValidator without any safety or persistence.
// Only use it for testing.
type MockPV struct {
	privKey              crypto.PrivKey
	breakProposalSigning bool
	breakVoteSigning     bool
}

func NewMockPV() *MockPV {
	return &MockPV{ed25519.GenPrivKey(), false, false}
}

// NewMockPVWithParams allows one to create a MockPV instance, but with finer
// grained control over the operation of the mock validator. This is useful for
// mocking test failures.
func NewMockPVWithParams(privKey crypto.PrivKey, breakProposalSigning, breakVoteSigning bool) *MockPV {
	return &MockPV{privKey, breakProposalSigning, breakVoteSigning}
}

// Implements PrivValidator.
func (pv *MockPV) GetPubKey() crypto.PubKey {
	return pv.privKey.PubKey()
}

func (pv *MockPV) SignVote(chainID string, vote *Vote) error {
	return pv.SignData(chainID, vote)
}

func (pv *MockPV) SignProposal(chainID string, proposal *Proposal) error {
	return pv.SignData(chainID, proposal)
}

func (pv *MockPV) SignData(chainID string, data DataSigner) error {
	useChainID := chainID
	if pv.breakVoteSigning {
		useChainID = "incorrect-chain-id"
	}

	signBytes := data.SignBytes(useChainID)
	sig, err := pv.privKey.Sign(signBytes)
	if err != nil {
		return err
	}
	data.SetSignature(sig)
	return nil
}

// String returns a string representation of the MockPV.
func (pv *MockPV) String() string {
	addr := pv.GetPubKey().Address()
	return fmt.Sprintf("MockPV{%v}", addr)
}

// XXX: Implement.
func (pv *MockPV) DisableChecks() {
	// Currently this does nothing,
	// as MockPV has no safety checks at all.
}

type erroringMockPV struct {
	*MockPV
}

var ErroringMockPVErr = errors.New("erroringMockPV always returns an error")

// Implements PrivValidator.
func (pv *erroringMockPV) SignVote(chainID string, vote *Vote) error {
	return ErroringMockPVErr
}

// Implements PrivValidator.
func (pv *erroringMockPV) SignProposal(chainID string, proposal *Proposal) error {
	return ErroringMockPVErr
}

// NewErroringMockPV returns a MockPV that fails on each signing request. Again, for testing only.
func NewErroringMockPV() *erroringMockPV {
	return &erroringMockPV{&MockPV{ed25519.GenPrivKey(), false, false}}
}
