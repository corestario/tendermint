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
	DefaultBLSVerifierMasterPubKey = "Df+DAgEC/4QAAf+CAAAR/4EGAQEFUG9pbnQB/4IAAAD/hv+EAAH/gEaa2LoprFk0+K2z4mb7OWTJ1Gtd5LmCsrslgaYc7g31LBCoos5i1evy+j8F9rH5Taknr8KFvWGE83MwZTA579kYzizgrY9VGxQDFBe4eCRZ+6ppu42eSsKYYi/3Lf//cB/TbdlTzyRVz6lHwWn6lZqQhA6Eoa9q7bto2pltcWaZ"
	DefaultBLSVerifierPubKey       = "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4b/hgL/gEaa2LoprFk0+K2z4mb7OWTJ1Gtd5LmCsrslgaYc7g31LBCoos5i1evy+j8F9rH5Taknr8KFvWGE83MwZTA579kYzizgrY9VGxQDFBe4eCRZ+6ppu42eSsKYYi/3Lf//cB/TbdlTzyRVz6lHwWn6lZqQhA6Eoa9q7bto2pltcWaZAA=="
	DefaultBLSVerifierPrivKey      = "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACX/iAIgTx30WSwv2cXmC0ybf5OhX9RIHMog0dss+ecmfgAeVOIA"
)

var TestnetShares = map[int]*BLSShareJSON{
	0: {
		Pub:  DefaultBLSVerifierPubKey,
		Priv: DefaultBLSVerifierPrivKey,
	},
	1: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgECAf+AhEqOOuENmkWFU467Nd0qZHHkxg0svT/xNqy6XOkvx2k3LFbm1bBZBH4A/L1OP50RgDJSuvGMeZHt6/KZjyugeGrM7I0DDOI62fh2AsH1LosSsHEZnZ6V+kcnee5JkjkFIVcerwDkzdbz0dMQu5GlRVk+I+WAuh3RQk4dZX5S8HgA",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAECASBxLoheHSAjymeDXzysWu9zlO0fF3TCMXKYHVzjjtBDsAA=",
	},
	2: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgEEAf+AjM/WGGkNmEhbRh/rM93emzi0bQYbS4Xf83o32yYbDucO0/NTv1DK6yUqNRSpM+ycVmw2B4eRBJlUm+58i6ZIXAW0FNNZqVr0E2rCVVjhjE0/J8Lbo6vwzj8wJV+vF6c7GhkqbI6VFr3gUiY4L7Ohr7DTW9WM7W09BKEiS0ZNZB0A",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAEEASBY/CsprVGn5R3ThMFVOlJiXLr/FQjQKVuSVI8JYx/vOwA=",
	},
	3: {
		Pub:  "I/+FAwEBCFB1YlNoYXJlAf+GAAECAQFJAQQAAQFWAf+CAAAAEf+BBgEBBVBvaW50Af+CAAAA/4j/hgEGAf+AFv9ps05s4hf9xwtZ219eolKRjK2HtPe51nWAtW7mRuV4Mfob2zy9ivFcjGBYvbvCqtxbYxaP4F4FW0bGDhp1FArrZdPTPDo8gCUQS0jOKHOGE0QFdJJX/1n8sFFOsTi2EsBwHihjql06YIRx0bxHTRXooEL9fqrsf43gqJ1j8ekA",
		Priv: "I/+HAwEBCFByaVNoYXJlAf+IAAECAQFJAQQAAQFWAf+KAAAAEv+JBgEBBlNjYWxhcgH/igAAACf/iAEGASBAyc31PYMr/9QjqkX+GbVRJIjfEpzeIUSMi8EvN2+axgA=",
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
