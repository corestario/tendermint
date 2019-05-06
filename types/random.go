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

	"github.com/tendermint/tendermint/crypto"

	"github.com/pkg/errors"
	"go.dedis.ch/kyber"
	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/share"
	"go.dedis.ch/kyber/sign/bls"
	"go.dedis.ch/kyber/sign/tbls"
)

const (
	storeMasterKey = "master.pub"
	storeShare     = "share.%s"
)

const (
	DefaultBLSVerifierMasterPubKey = "Df+DAgEC/4QAAf+CAAAR/4EGAQEFUG9pbnQB/4IAAAD/hv+EAAH/gIY3tdQP8FmTwMzIYDr25oWviHQ72h2GHHP9z6tMR8KTCH0Loz5OJFs06ARy7BlWGoY7X82GhfdfQpMbi8kndG6Ii59k0+4xj8m15SCb5iN6m1j0UuR1TVhA4XCPrgranjnQwnEBXlNAQVOEYLu164o3yVnA0BCMZV6oAarX0esr"
	DefaultBLSVerifierPubKey       = "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4b/hgL/gIY3tdQP8FmTwMzIYDr25oWviHQ72h2GHHP9z6tMR8KTCH0Loz5OJFs06ARy7BlWGoY7X82GhfdfQpMbi8kndG6Ii59k0+4xj8m15SCb5iN6m1j0UuR1TVhA4XCPrgranjnQwnEBXlNAQVOEYLu164o3yVnA0BCMZV6oAarX0esrAA=="
	DefaultBLSVerifierPrivKey      = "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACX/iAIgI+Ls8rNHyYSBKfe4iiNJa4wGngv/7kn/URRQ3XaZD1gA"
)

var TestnetShares = map[int]*BLSShareJSON{
	0: {
		Pub:  DefaultBLSVerifierPubKey,
		Priv: DefaultBLSVerifierPrivKey,
	},
	1: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgECAf+Ahje11A/wWZPAzMhgOvbmha+IdDvaHYYcc/3Pq0xHwpMIfQujPk4kWzToBHLsGVYahjtfzYaF919CkxuLySd0boiLn2TT7jGPybXlIJvmI3qbWPRS5HVNWEDhcI+uCtqeOdDCcQFeU0BBU4Rgu7XrijfJWcDQEIxlXqgBqtfR6ysA",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAECASAj4uzys0fJhIEp97iKI0lrjAaeC//uSf9RFFDddpkPWAA=",
	},
	2: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgEEAf+Ahje11A/wWZPAzMhgOvbmha+IdDvaHYYcc/3Pq0xHwpMIfQujPk4kWzToBHLsGVYahjtfzYaF919CkxuLySd0boiLn2TT7jGPybXlIJvmI3qbWPRS5HVNWEDhcI+uCtqeOdDCcQFeU0BBU4Rgu7XrijfJWcDQEIxlXqgBqtfR6ysA",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAEEASAj4uzys0fJhIEp97iKI0lrjAaeC//uSf9RFFDddpkPWAA=",
	},
	3: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgEGAf+Ahje11A/wWZPAzMhgOvbmha+IdDvaHYYcc/3Pq0xHwpMIfQujPk4kWzToBHLsGVYahjtfzYaF919CkxuLySd0boiLn2TT7jGPybXlIJvmI3qbWPRS5HVNWEDhcI+uCtqeOdDCcQFeU0BBU4Rgu7XrijfJWcDQEIxlXqgBqtfR6ysA",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAEGASAj4uzys0fJhIEp97iKI0lrjAaeC//uSf9RFFDddpkPWAA=",
	},
}

type BLSKeyring struct {
	T            int               // Threshold
	N            int               // Number of shares
	Shares       map[int]*BLSShare // mapping from share ID to share
	MasterPubKey *share.PubPoly    // Public key used to verify individual and aggregate signatures
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
		priPoly = share.NewPriPoly(suite.G2(), t, secret, suite.RandomStream())
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
	Pub  *share.PubShare
	Priv *share.PriShare
}

type BLSShareJSON struct {
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
		Pub:  base64.StdEncoding.EncodeToString(pubBuf.Bytes()),
		Priv: base64.StdEncoding.EncodeToString(privBuf.Bytes()),
	}, nil
}

func (m *BLSShareJSON) Deserialize() (*BLSShare, error) {
	pubBytes, err := base64.StdEncoding.DecodeString(m.Pub)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode public key: %v", err)
	}
	pubKey, pubDec := &share.PubShare{V: bn256.NewSuite().G2().Point()}, gob.NewDecoder(bytes.NewBuffer(pubBytes))
	if err := pubDec.Decode(pubKey); err != nil {
		return nil, fmt.Errorf("failed to decode public key: %v", err)
	}

	privBytes, err := base64.StdEncoding.DecodeString(m.Priv)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode private key: %v", err)
	}
	privKey, privDec := &share.PriShare{V: bn256.NewSuite().G1().Scalar()}, gob.NewDecoder(bytes.NewBuffer(privBytes))
	if err := privDec.Decode(privKey); err != nil {
		return nil, fmt.Errorf("failed to decode private key: %v", err)
	}

	return &BLSShare{
		Pub:  pubKey,
		Priv: privKey,
	}, nil
}

func LoadBLSShareJSON(path string) (*BLSShareJSON, error) {
	var sh BLSShareJSON
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	err = json.NewDecoder(f).Decode(&sh)
	return &sh, err
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

	for id, keypair := range keyring.Shares {
		skp, err := NewBLSShareJSON(keypair)
		if err != nil {
			return fmt.Errorf("failed to serialize keypair #%d: %v", id, err)
		}
		data, err := json.Marshal(skp)
		if err != nil {
			return fmt.Errorf("failed to marshal keypair for id %d: %v", keypair.ID, err)
		}

		fileName := fmt.Sprintf(storeShare, fmt.Sprintf("%d", id))
		if err := ioutil.WriteFile(filepath.Join(targetDir, fileName), data, 0644); err != nil {
			return fmt.Errorf("failed to write key pair for id %d to disk: %v", id, err)
		}
	}

	return nil
}

func LoadPubKey(base64Key string, numHolders int) (*share.PubPoly, error) {
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

	return share.NewPubPoly(bn256.NewSuite().G2(), nil, commits), nil
}

type Verifier interface {
	Sign(data []byte) ([]byte, error)
	VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error
	VerifyRandomData(prevRandomData, currRandomData []byte) error
	Recover(msg []byte, precommits []*Vote) ([]byte, error)
}

type BLSVerifier struct {
	Keypair      *BLSShare // This verifier's BLSShare.
	masterPubKey *share.PubPoly
	suiteG1      *bn256.Suite
	suiteG2      *bn256.Suite
	t            int
	n            int
}

func NewBLSVerifier(masterPubKey *share.PubPoly, sh *BLSShare, t, n int) *BLSVerifier {
	return &BLSVerifier{
		masterPubKey: masterPubKey,
		Keypair:      sh,
		suiteG1:      bn256.NewSuiteG1(),
		suiteG2:      bn256.NewSuiteG2(),
		t:            t,
		n:            n,
	}
}

func (m *BLSVerifier) Sign(data []byte) ([]byte, error) {
	sig, err := tbls.Sign(m.suiteG1, m.Keypair.Priv, data)
	if err != nil {
		return nil, fmt.Errorf("failed to sing random data with key %v %v with error %v", m.Keypair.Pub, data, err)
	}

	return sig, nil
}

func (m *BLSVerifier) VerifyRandomShare(addr string, prevRandomData, currRandomData []byte) error {
	// Check that the signature itself is correct for this validator.
	if err := tbls.Verify(m.suiteG1, m.masterPubKey, prevRandomData, currRandomData); err != nil {
		return fmt.Errorf("signature of share is corrupt: %v. prev random: %v; current random: %v", err, prevRandomData, currRandomData)
	}

	return nil
}

func (m *BLSVerifier) VerifyRandomData(prevRandomData, currRandomData []byte) error {
	if err := bls.Verify(m.suiteG1, m.masterPubKey.Commit(), prevRandomData, currRandomData); err != nil {
		return fmt.Errorf("signature is corrupt: %v. prev random: %v; current random: %v", err, prevRandomData, currRandomData)
	}

	return nil
}

func (m *BLSVerifier) Recover(msg []byte, precommits []*Vote) ([]byte, error) {
	var sigs [][]byte
	for _, precommit := range precommits {
		// Nil votes do exist, keep that in mind.
		if precommit == nil || len(precommit.BlockID.Hash) == 0 || len(precommit.BLSSignature) == 0 {
			continue
		}

		sigs = append(sigs, precommit.BLSSignature)
	}

	aggrSig, err := tbls.Recover(m.suiteG1, m.masterPubKey, msg, sigs, m.t, m.n)
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
		Pub:  DefaultBLSVerifierPubKey,
		Priv: DefaultBLSVerifierPrivKey,
	}
	sh, err := shareJSON.Deserialize()
	if err != nil {
		panic(err)
	}

	pubPoly, err := LoadPubKey(DefaultBLSVerifierMasterPubKey, n)
	if err != nil {
		panic(err)
	}
	return NewBLSVerifier(pubPoly, sh, t, n)
}

type MockVerifier struct{}

func (m *MockVerifier) Sign(data []byte) ([]byte, error) {
	return data, nil
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

type DKGDataType int

const (
	DKGPubKey DKGDataType = iota
	DKGDeal
	DKGResponse
	DKGJustification
	DKGCommits
	DKGComplaint
	DKGReconstructCommit
)

type DKGData struct {
	Type        DKGDataType
	Addr        []byte
	RoundID     int
	Data        []byte // Data is going to keep serialized kyber objects.
	ToIndex     int    // ID of the participant for whom the message is; might be not set
	NumEntities int    // Number of sub-entities in the Data array, sometimes required for unmarshaling.
}

func (m *DKGData) GetAddrString() string {
	return crypto.Address(m.Addr).String()
}

func (m *DKGData) ValidateBasic() error {
	return nil
}
