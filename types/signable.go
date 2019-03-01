package types

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/dgaming-crypto/go/bls"
	cmn "github.com/tendermint/tendermint/libs/common"
)

var (
	// MaxSignatureSize is a maximum allowed signature size for the Proposal
	// and Vote.
	// XXX: secp256k1 does not have Size nor MaxSize defined.
	MaxSignatureSize = cmn.MaxInt(ed25519.SignatureSize, 64)
)

const (
	SolitaireBLSVerifierMasterPubKey = "1 2209a5a1c73b404d35fbdbaf1e92b8cda2e0d83a31d6950bf4273dd3c1ff998dad224aaa82bc9a06a0e174296aef41cb 1d0c1755a72ea9f2d235b76837f3ec9603ec30a15665aa98fedb61c86720c9cd573de0f362bc3557f891888a6a483441 126f1f76b8519f138e51d44a74e21e942dc33b5b2860dc606539099ae6df3ebe80f7ef60667676cd9f5585ef499fbeb5 cb1daa954be1452e14c7535f7de36de298c3c7104fdc92b2c2392edf0918f9aa9a264dd7ddaf41996532b006b7f7308"
	SolitaireBLSVerifierID           = "1"
	SolitaireBLSVerifierPubKey       = SolitaireBLSVerifierMasterPubKey
	SolitaireBLSVerifierPrivKey      = "84349a2971899a45aa6af183fe2ec287a52b143c260b460c81f01e011458e9221a65406a23683a375ba84e87d3e6a7f"
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

type Verifier interface {
	Sign(data []byte) []byte
	VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error
	VerifyRandomData(prevRandomData, currRandomData []byte) error
	Recover(precommits []*Vote) ([]byte, error)
}

type BLSVerifier struct {
	Others       map[string]*bls.Keypair // Other validators' keys.
	Keypair      *bls.Keypair            // This verifier's Keypair.
	masterPubKey *bls.PublicKey
}

func NewBLSVerifier(masterPubKey *bls.PublicKey, keypair *bls.Keypair, others map[string]*bls.Keypair) *BLSVerifier {
	return &BLSVerifier{
		masterPubKey: masterPubKey,
		Keypair:      keypair,
		Others:       others,
	}
}

func (m *BLSVerifier) Sign(data []byte) []byte {
	return m.Keypair.Priv.Sign(string(data)).Serialize()
}

func (m *BLSVerifier) VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error {
	keypair, ok := m.Others[addr]
	if !ok {
		return fmt.Errorf("found no keypair for address %s", addr)
	}

	sign := new(bls.Sign)
	if err := sign.Deserialize(currRandomData); err != nil {
		return fmt.Errorf("failed to deserialize current random data: %v", err)
	}

	if !sign.Verify(keypair.Pub, string(prevRandomData)) {
		return fmt.Errorf("current signature is corrupt:\nsign %v\naddress %v\n pubKey %v\nprevRandomStr %q\nprevRandom %v", sign.GetHexString(), addr, keypair.Pub.GetHexString(), string(prevRandomData), prevRandomData)
	}

	return nil
}

func (m *BLSVerifier) VerifyRandomData(prevRandomData, currRandomData []byte) error {
	sign := new(bls.Sign)
	if err := sign.Deserialize(currRandomData); err != nil {
		return fmt.Errorf("failed to deserialize current random data: %v", err)
	}

	if !sign.Verify(m.masterPubKey, string(prevRandomData)) {
		return errors.New("current signature is corrupt")
	}

	return nil
}

func (m *BLSVerifier) Recover(precommits []*Vote) ([]byte, error) {
	var (
		signs []bls.Sign
		ids   []bls.ID
	)

	for _, precommit := range precommits {
		// Nil votes do exist, keep that in mind.
		if precommit == nil || len(precommit.BlockID.Hash) == 0 || len(precommit.BLSSignature) == 0 {
			continue
		}

		addr, sign := precommit.ValidatorAddress.String(), new(bls.Sign)
		if err := sign.Deserialize(precommit.BLSSignature); err != nil {
			return nil, fmt.Errorf("failed to read signature %s from %s: %v",
				string(precommit.BLSSignature), addr, err)
		}

		signs, ids = append(signs, *sign), append(ids, *m.Others[addr].Id)
	}

	aggrSign := new(bls.Sign)
	if err := aggrSign.Recover(signs, ids); err != nil {
		return nil, fmt.Errorf("failed to recover aggregate signature: %v", err)
	}

	return aggrSign.Serialize(), nil
}

// NewTestBLSVerifier creates a BLSVerifier with a 1-of-2 key set that doesn't require any
// other signatures but his own.
// Keys are hardcoded to make tests output more deterministic.
func NewTestBLSVerifier(addr string) *BLSVerifier {
	var (
		id      = new(bls.ID)
		_       = id.SetHexString(SolitaireBLSVerifierID)
		pub     = new(bls.PublicKey)
		_       = pub.SetHexString(SolitaireBLSVerifierPubKey)
		priv    = new(bls.SecretKey)
		_       = priv.SetHexString(SolitaireBLSVerifierPrivKey)
		keypair = &bls.Keypair{
			Id:   id,
			Pub:  pub,
			Priv: priv,
		}
		others = map[string]*bls.Keypair{}
	)
	others[addr] = keypair

	// In the case of threshold = 1, verifier's pub key and master pub key are the same.
	return NewBLSVerifier(pub, keypair, others)
}

type MockVerifier struct{}

func (m *MockVerifier) Sign(data []byte) []byte {
	return data
}
func (m *MockVerifier) VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error {
	return nil
}
func (m *MockVerifier) VerifyRandomData(prevRandomData, currRandomData []byte) error {
	return nil
}
func (m *MockVerifier) Recover(precommits []*Vote) ([]byte, error) {
	return []byte{}, nil
}
