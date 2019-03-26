package types

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"go.dedis.ch/kyber/pairing/bn256"
	"go.dedis.ch/kyber/sign/bls"
	"go.dedis.ch/kyber/sign/tbls"

	cmn "github.com/tendermint/tendermint/libs/common"
)

func TestDumpLoad(t *testing.T) {
	var targetDir = filepath.Join(os.TempDir(), "go-bls-test")
	defer func() {
		if err := os.RemoveAll(targetDir); err != nil {
			t.Log(err)
		}
	}()

	if err := os.Mkdir(targetDir, 0777); err != nil {
		t.Errorf("failed to create test directory: %v", err)
		return
	}

	var threshold, numHolders = 10, 16
	keyring, err := NewBLSKeyring(threshold, numHolders)
	if err != nil {
		t.Errorf("failed to generate keyring: %v", err)
		return
	}

	if err := DumpBLSKeyring(keyring, targetDir); err != nil {
		t.Errorf("failed to dump keyring: %v", err)
		return
	}

	pubKeyBytes, err := ioutil.ReadFile(filepath.Join(targetDir, storeMasterKey))
	if err != nil {
		t.Errorf("failed to read public key file: %v", err)
	}
	if _, err := LoadPubKey(string(pubKeyBytes), numHolders); err != nil {
		t.Errorf("failed to load public key: %v", err)
		return
	}

	keyPairBytes, err := ioutil.ReadFile(filepath.Join(targetDir, fmt.Sprintf(storeShare, "5")))
	if err != nil {
		t.Errorf("failed to read keypair file: %v", err)
		return
	}
	skp := &BLSShareJSON{}
	if err := json.Unmarshal(keyPairBytes, skp); err != nil {
		t.Errorf("failed to unmarshal to BLSShareJSON: %v", err)
		return
	}

	_, err = skp.Deserialize()
	if err != nil {
		t.Errorf("failed to load keypair: %v", err)
		return
	}
}

func TestRecover(t *testing.T) {
	var (
		testVerifier = NewTestBLSVerifier("test")
		msg          = []byte("test")
	)
	sign, err := testVerifier.Sign(msg)
	if err != nil {
		t.Errorf("failed to sing with test verifier: %v", err)
		return
	}

	_, err = testVerifier.Recover(msg, []*Vote{
		{
			BlockID: BlockID{
				Hash: cmn.HexBytes("text"),
			},
			ValidatorAddress: Address("test"),
			BLSSignature:     sign,
		},
	})
	if err != nil {
		t.Errorf("failed to sing with test verifier: %v", err)
		return
	}
}

func TestRecover2of4(t *testing.T) {
	var (
		pubKey, _ = LoadPubKey(DefaultBLSVerifierMasterPubKey, 4)
		share0, _ = TestnetShares[0].Deserialize()
		share1, _ = TestnetShares[1].Deserialize()
		msg       = []byte(InitialRandomData)
		suite     = bn256.NewSuite()
		sig0, _   = tbls.Sign(suite, share0.Priv, msg)
		sig1, _   = tbls.Sign(suite, share1.Priv, msg)
	)

	aggrSig, err := tbls.Recover(suite, pubKey, msg, [][]byte{sig0, sig1}, 2, 4)
	if err != nil {
		t.Errorf("aggr sign: %v", err)
		return
	}

	if err := bls.Verify(suite, pubKey.Commit(), msg, aggrSig); err != nil {
		t.Errorf("verify: %v", err)
	}
}
