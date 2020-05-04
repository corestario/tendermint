package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"go.dedis.ch/kyber/v3/pairing/bn256"
	"go.dedis.ch/kyber/v3/sign/bls"
	"go.dedis.ch/kyber/v3/sign/tbls"

	dkgBLS "github.com/corestario/dkglib/lib/blsShare"
)

const (
	storeMasterKey = "master.pub"
	storeShare     = "share.%s"
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
	keyring, err := dkgBLS.NewBLSKeyring(threshold, numHolders)
	if err != nil {
		t.Errorf("failed to generate keyring: %v", err)
		return
	}

	if err := dkgBLS.DumpBLSKeyring(keyring, targetDir); err != nil {
		t.Errorf("failed to dump keyring: %v", err)
		return
	}

	pubKeyBytes, err := ioutil.ReadFile(filepath.Join(targetDir, storeMasterKey))
	if err != nil {
		t.Errorf("failed to read public key file: %v", err)
	}
	if _, err := dkgBLS.LoadPubKey(string(pubKeyBytes), numHolders); err != nil {
		t.Errorf("failed to load public key: %v", err)
		return
	}

	keyPairBytes, err := ioutil.ReadFile(filepath.Join(targetDir, fmt.Sprintf(storeShare, "1")))
	if err != nil {
		t.Errorf("failed to read keypair file: %v", err)
		return
	}
	skp := &dkgBLS.BLSShareJSON{}
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
	testCases := []struct{ t, n int }{
		{1, 1},
		{1, 4},
		{2, 4},
		{3, 4},
		{4, 4},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d of %d", tc.t, tc.n), func(t *testing.T) {
			testRecover(t, tc.t, tc.n)
		})
	}
}

func testRecover(st *testing.T, t, n int) {
	var testVerifiers []*dkgBLS.BLSVerifier
	for i := 0; i < n; i++ {
		testVerifiers = append(testVerifiers, dkgBLS.NewTestBLSVerifierByID("TestRecover4", i, t, n))
	}

	var err error
	signs := make([][]byte, n)
	votes := make([]dkgBLS.BLSSigner, n)
	msg := []byte("test")

	for i := 0; i < n; i++ {
		signs[i], err = testVerifiers[i].Sign(msg)
		if err != nil {
			st.Errorf("failed to sing with test verifier: %v", err)
			return
		}

		votes = append(votes, &Vote{
			BlockID: BlockID{
				Hash: tmbytes.HexBytes("text"),
			},
			ValidatorAddress: Address("test"),
			BLSSignature:     signs[i],
		})

		for j := 0; j <= i; j++ {
			if err = testVerifiers[i].VerifyRandomShare("", msg, signs[j]); err != nil {
				st.Errorf("failed to verify share %d by verifier %d: %v", j, i, err)
				return
			}
		}
	}

	blsSigns := make([][]byte, n)
	for i := 0; i < n; i++ {
		blsSigns[i], err = testVerifiers[i].Recover(msg, votes)
		if err != nil {
			st.Errorf("failed to sing with test verifier: %v", err)
			return
		}

		if len(blsSigns[i]) == 0 {
			st.Errorf("failed to sing with test verifier - empty signature: %v", err)
			return
		}
	}

	for i := 1; i < n; i++ {
		if !bytes.Equal(blsSigns[0], blsSigns[i]) {
			st.Errorf("different bls signatures 0(%v) and %d(%v)", blsSigns[0], i, blsSigns[i])
			return
		}

		if err = testVerifiers[0].VerifyRandomData(msg, blsSigns[i]); err != nil {
			st.Errorf("failed to verify share %d by verifier 0: %v", i, err)
			return
		}
	}
}

func TestRecover2of4(t *testing.T) {
	var (
		pubKey, _ = dkgBLS.LoadPubKey(dkgBLS.TestnetMasterPubKey, 4)
		share0, _ = dkgBLS.TestnetShares[0].Deserialize()
		share1, _ = dkgBLS.TestnetShares[1].Deserialize()
		share2, _ = dkgBLS.TestnetShares[2].Deserialize()
		msg       = []byte(InitialRandomData)
		suite     = bn256.NewSuite()
		sig0, _   = tbls.Sign(suite, share0.Priv, msg)
		sig1, _   = tbls.Sign(suite, share1.Priv, msg)
		sig2, _   = tbls.Sign(suite, share2.Priv, msg)
	)

	aggrSig, err := tbls.Recover(suite, pubKey, msg, [][]byte{sig0, sig1, sig2}, 3, 4)
	if err != nil {
		t.Errorf("aggr sign: %v", err)
		return
	}

	if err := bls.Verify(suite, pubKey.Commit(), msg, aggrSig); err != nil {
		t.Errorf("verify: %v", err)
	}
}
