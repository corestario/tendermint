package types

import (
	"bytes"
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
	if err := os.RemoveAll(targetDir); err != nil {
		t.Log(err)
	}
	t.Log(targetDir)

	if err := os.Mkdir(targetDir, 0777); err != nil {
		t.Errorf("failed to create test directory: %v", err)
		return
	}

	var threshold, numHolders = 1, 4
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

	keyPairBytes, err := ioutil.ReadFile(filepath.Join(targetDir, fmt.Sprintf(storeShare, "1")))
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

func TestRecover4(t *testing.T) {
	const N = 4
	const T = 3
	msg := []byte("test")

	var testVerifiers []*BLSVerifier
	for i := 0; i < N; i++ {
		testVerifiers = append(testVerifiers, NewTestBLSVerifierByID("TestRecover4", i, T, N))
	}

	signs := make([][]byte, N)
	votes := make([]*Vote, N)
	var err error

	for i := 0; i < N; i++ {
		signs[i], err = testVerifiers[i].Sign(msg)
		if err != nil {
			t.Errorf("failed to sing with test verifier: %v", err)
			return
		}

		votes = append(votes, &Vote{
			BlockID: BlockID{
				Hash: cmn.HexBytes("text"),
			},
			ValidatorAddress: Address("test"),
			BLSSignature:     signs[i],
		})

		for j := 0; j <= i; j++ {
			if err = testVerifiers[i].VerifyRandomShare("", msg, signs[j]); err != nil {
				t.Errorf("failed to verify share %d by verifier %d: %v", j, i, err)
				return
			}
		}
	}

	blsSigns := make([][]byte, N)
	for i := 0; i < N; i++ {
		blsSigns[i], err = testVerifiers[i].Recover(msg, votes)
		if err != nil {
			t.Errorf("failed to sing with test verifier: %v", err)
			return
		}

		if len(blsSigns[i]) == 0 {
			t.Errorf("failed to sing with test verifier - empty signature: %v", err)
			return
		}
	}

	for i := 1; i < N; i++ {
		if !bytes.Equal(blsSigns[0], blsSigns[i]) {
			t.Errorf("different bls signatures 0(%v) and %d(%v)", blsSigns[0], i, blsSigns[i])
			return
		}

		if err = testVerifiers[0].VerifyRandomData(msg, blsSigns[i]); err != nil {
			t.Errorf("failed to verify share %d by verifier 0: %v", i, err)
			return
		}
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

	aggrSig, err := tbls.Recover(suite, pubKey, msg, [][]byte{sig0, sig1}, 1, 4)
	if err != nil {
		t.Errorf("aggr sign: %v", err)
		return
	}

	if err := bls.Verify(suite, pubKey.Commit(), msg, aggrSig); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestRecover3of4(t *testing.T) {
	var (
		pubKey, _ = LoadPubKey(DefaultBLSVerifierMasterPubKey, 4)
		msg       = []byte(InitialRandomData)
		suite     = bn256.NewSuite()
	)

	for i := 0; i < 4; i++ {
		var shares []*BLSShare
		for j := i; j < 4; j++ {
			var sigs [][]byte
			share, _ := TestnetShares[j].Deserialize()
			shares = append(shares, share)
			for k := range shares {
				sig, err := tbls.Sign(suite, shares[k].Priv, msg)
				if err != nil {
					t.Fatal(err)
				}
				sigs = append(sigs, sig)
				aggrSig, err := tbls.Recover(suite, pubKey, msg, sigs, 1, 4)
				if err != nil {
					t.Errorf("aggr sign: %v", err)
					return
				}

				if err := bls.Verify(suite, pubKey.Commit(), msg, aggrSig); err != nil {
					t.Errorf("verify: %v", err)
				}
			}
		}

	}

}
