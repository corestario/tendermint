package types

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/tendermint/tendermint/libs/common"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/share"
	kyberShare "go.dedis.ch/kyber/share"
	"go.dedis.ch/kyber/sign/bls"
	"go.dedis.ch/kyber/sign/tbls"
)

const (
	storeMasterKey = "master.pub"
	storeShare     = "share.%s"
)

const (
	DefaultBLSVerifierMasterPubKey = "Df+DAgEC/4QAAf+CAAAR/4EGAQEFUG9pbnQB/4IAAAD/hv+EAAH/gG9Pz5sOyRmxdttuuCOwK+efAvhrO9nTVk+JrBLW1EscSDz3QBnKSWTCHb26RDbQGJEfo2Utq29y/uzFHKqrHNAzlbSe9+0Nv8sCldtXiPz96STqRp1Nxtso7Cnk2Z+q1lu39AFVYFluEUbpKWcdXXAqupgfHuyEwiCLjNDHoc/Q"
	DefaultBLSVerifierID           = 0
	DefaultBLSVerifierPubKey       = "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4b/hgL/gG9Pz5sOyRmxdttuuCOwK+efAvhrO9nTVk+JrBLW1EscSDz3QBnKSWTCHb26RDbQGJEfo2Utq29y/uzFHKqrHNAzlbSe9+0Nv8sCldtXiPz96STqRp1Nxtso7Cnk2Z+q1lu39AFVYFluEUbpKWcdXXAqupgfHuyEwiCLjNDHoc/QAA=="
	DefaultBLSVerifierPrivKey      = "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACX/iAIghVFMQNE4GNFCGPpzYXJ8lqUnHA0IlIefA3j+lvDdoUYA"
)

type BLSKeyring struct {
	T            int                 // Threshold
	N            int                 // Number of shares
	Shares       map[int]*BLSShare   // mapping from share ID to share
	MasterPubKey *kyberShare.PubPoly // Public key used to verify individual and aggregate signatures
}

// NewBLSKeyring generates a tbls keyring (master key, t-of-n shares).
func NewBLSKeyring(t, n int) (*BLSKeyring, error) {
	if t > n {
		return nil, errors.New("threshold can not be greater that number of holders")
	}
	if t < 1 {
		return nil, errors.New("threshold can not be < 1")
	}
	if n < 1 {
		return nil, errors.New("number of holders can not be < 1")
	}
	var (
		suite   = bn256.NewSuite()
		secret  = suite.G1().Scalar().Pick(suite.RandomStream())
		priPoly = kyberShare.NewPriPoly(suite.G2(), t, secret, suite.RandomStream())
		pubPoly = priPoly.Commit(suite.G2().Point().Base())
		keyring = &BLSKeyring{
			N:            n,
			T:            t,
			MasterPubKey: pubPoly,
			Shares:       make(map[int]*BLSShare),
		}
	)
	pubShares, privShares := pubPoly.Shares(n), priPoly.Shares(n)
	for id := 0; id < n; id++ {
		keyring.Shares[id] = &BLSShare{
			ID:   id,
			Pub:  pubShares[id],
			Priv: privShares[id],
		}
	}

	return keyring, nil
}

type BLSShare struct {
	ID   int
	Pub  *kyberShare.PubShare
	Priv *kyberShare.PriShare
}

type BLSShareJSON struct {
	ID   int    `json:"id"`
	Pub  string `json:"pub"`
	Priv string `json:"priv"`
}

func NewBLSShareJSON(keypair *BLSShare) (*BLSShareJSON, error) {
	pubBuf := bytes.NewBuffer(nil)
	pubEnc := gob.NewEncoder(pubBuf)
	if err := pubEnc.Encode(keypair.Pub); err != nil {
		return nil, fmt.Errorf("failed to encode public key: %v", err)
	}

	privBuf := bytes.NewBuffer(nil)
	privEnc := gob.NewEncoder(privBuf)
	if err := privEnc.Encode(keypair.Priv); err != nil {
		return nil, fmt.Errorf("failed to encode private key: %v", err)
	}

	return &BLSShareJSON{
		ID:   keypair.ID,
		Pub:  base64.StdEncoding.EncodeToString(pubBuf.Bytes()),
		Priv: base64.StdEncoding.EncodeToString(privBuf.Bytes()),
	}, nil
}

func (m *BLSShareJSON) Deserialize() (*BLSShare, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(m.Pub)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode public key: %v", err)
	}
	pubKey, pubDec := &kyberShare.PubShare{V: bn256.NewSuite().G2().Point()}, gob.NewDecoder(bytes.NewBuffer(pubBytes))
	if err := pubDec.Decode(pubKey); err != nil {
		return nil, fmt.Errorf("failed to decode public key: %v", err)
	}

	privBytes, err := base64.StdEncoding.DecodeString(m.Priv)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode private key: %v", err)
	}
	privKey, privDec := &kyberShare.PriShare{V: bn256.NewSuite().G1().Scalar()}, gob.NewDecoder(bytes.NewBuffer(privBytes))
	if err := privDec.Decode(privKey); err != nil {
		return nil, fmt.Errorf("failed to decode private key: %v", err)
	}

	return &BLSShare{
		ID:   m.ID,
		Pub:  pubKey,
		Priv: privKey,
	}, nil
}

func LoadBLSShareJSON(path string) (*BLSShareJSON, error) {
	var share BLSShareJSON
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	err = json.NewDecoder(f).Decode(&share)
	return &share, err
}

func DumpBLSKeyring(keyring *BLSKeyring, targetDir string) error {
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		return fmt.Errorf("failed to dump keyring, directory does not exist")
	}

	pubBuf := bytes.NewBuffer(nil)
	pubEnc := gob.NewEncoder(pubBuf)
	_, pubKeyCommits := keyring.MasterPubKey.Info()
	if err := pubEnc.Encode(pubKeyCommits); err != nil {
		return fmt.Errorf("failed to encode master public key: %v", err)
	}
	base64PubKeyCommits := base64.StdEncoding.EncodeToString(pubBuf.Bytes())
	if err := ioutil.WriteFile(filepath.Join(targetDir, storeMasterKey), []byte(base64PubKeyCommits), 0644); err != nil {
		return fmt.Errorf("failed to write master public key to disk: %v", err)
	}

	for _, keypair := range keyring.Shares {
		skp, err := NewBLSShareJSON(keypair)
		if err != nil {
			return fmt.Errorf("failed to serialize keypair #%d: %v", skp.ID, err)
		}
		data, err := json.Marshal(skp)
		if err != nil {
			return fmt.Errorf("failed to marshal keypair for id %d: %v", keypair.ID, err)
		}

		fileName := fmt.Sprintf(storeShare, fmt.Sprintf("%d", skp.ID))
		if err := ioutil.WriteFile(filepath.Join(targetDir, fileName), data, 0644); err != nil {
			return fmt.Errorf("failed to write key pair for id %d to disk: %v", skp.ID, err)
		}
	}

	return nil
}

func LoadPubKey(base64Key string, numHolders int) (*kyberShare.PubPoly, error) {
	suite := bn256.NewSuite()

	keyBytes, err := base64.StdEncoding.DecodeString(string(base64Key))
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode master public key: %v", err)
	}

	var commits = make([]kyber.Point, numHolders)
	for idx := range commits {
		commits[idx] = suite.G2().Point()
	}
	dec := gob.NewDecoder(bytes.NewBuffer(keyBytes))
	if err := dec.Decode(&commits); err != nil {
		return nil, fmt.Errorf("failed to decode public key: %v", err)
	}

	return kyberShare.NewPubPoly(bn256.NewSuite().G2(), nil, commits), nil
}

type Verifier interface {
	Sign(data []byte) []byte
	VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error
	VerifyRandomData(prevRandomData, currRandomData []byte) error
	Recover(msg []byte, precommits []*Vote) ([]byte, error)
}

type BLSVerifier struct {
	Others       map[string]int // string(crypto.Address) -> verifier's tbls ID.
	Keypair      *BLSShare      // This verifier's BLSShare.
	masterPubKey *share.PubPoly
	suite        *bn256.Suite
	t            int
	n            int
}

func NewBLSVerifier(masterPubKey *share.PubPoly, keypair *BLSShare, others map[string]int, t, n int) *BLSVerifier {
	return &BLSVerifier{
		masterPubKey: masterPubKey,
		Keypair:      keypair,
		Others:       others,
		suite:        bn256.NewSuite(),
		t:            t,
		n:            n,
	}
}

func (m *BLSVerifier) Sign(data []byte) []byte {
	sig, err := tbls.Sign(m.suite, m.Keypair.Priv, data)
	if err != nil {
		common.PanicSanity("failed to sing random data")
	}

	return sig
}

func (m *BLSVerifier) VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error {
	expectedID, ok := m.Others[addr]
	if !ok {
		return fmt.Errorf("found no keypair for address %s", addr)
	}

	// Check that we got the signature from the right validator.
	s := tbls.SigShare(currRandomData)
	id, err := s.Index()
	if err != nil {
		return fmt.Errorf("failed to get validator's index from signature: %v", err)
	}
	if expectedID != id {
		return fmt.Errorf("got signature from unexpected validator")
	}

	// Check that the signature itself is correct for this validator.
	if err := tbls.Verify(m.suite, m.masterPubKey, prevRandomData, currRandomData); err != nil {
		return fmt.Errorf("signature is corrupt: %v", err)
	}

	return nil
}

func (m *BLSVerifier) VerifyRandomData(prevRandomData, currRandomData []byte) error {
	if err := bls.Verify(m.suite, m.masterPubKey.Commit(), prevRandomData, currRandomData); err != nil {
		return fmt.Errorf("signature is corrupt: %v", err)
	}

	return nil
}

func (m *BLSVerifier) Recover(msg []byte, precommits []*Vote) ([]byte, error) {
	var sigs [][]byte
	for _, precommit := range precommits {
		// Nil votes do exist, keep that in mind.
		if precommit == nil {
			continue
		}

		sigs = append(sigs, precommit.BLSSignature)
	}

	aggrSig, err := tbls.Recover(m.suite, m.masterPubKey, msg, sigs, m.t, m.n)
	if err != nil {
		return nil, fmt.Errorf("failed to recover aggregate signature: %v", err)
	}

	return aggrSig, nil
}

// NewTestBLSVerifier creates a BLSVerifier with a 1-of-2 key set that doesn't require any
// other signatures but his own.
// Keys are hardcoded to make tests output more deterministic.
func NewTestBLSVerifier(addr string) *BLSVerifier {
	t, n := 1, 4
	shareJSON := BLSShareJSON{
		ID:   DefaultBLSVerifierID,
		Pub:  DefaultBLSVerifierPubKey,
		Priv: DefaultBLSVerifierPrivKey,
	}
	share, err := shareJSON.Deserialize()
	if err != nil {
		panic(err)
	}

	pubPoly, err := LoadPubKey(DefaultBLSVerifierMasterPubKey, n)
	if err != nil {
		panic(err)
	}
	others := map[string]int{
		addr: DefaultBLSVerifierID,
	}
	return NewBLSVerifier(pubPoly, share, others, t, n)
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
func (m *MockVerifier) Recover(msg []byte, precommits []*Vote) ([]byte, error) {
	return []byte{}, nil
}

type DKGDataType byte

const (
	DKGDeal DKGDataType = iota
	DKGResponse
	DKGJustification
	DKGCommit
	DKGComplaint
	DKGReconstructCommit
)

type DKGData struct {
	Type          DKGDataType
	ParticipantID int
	RoundID       int
	Data          []byte         // Data is going to keep serialized kyber objects.
	Meta          map[int][]byte // Meta can hold any additional data.
}

func (m *DKGData) ValidateBasic() error {
	return nil
}
