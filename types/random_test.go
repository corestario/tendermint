package types

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestOMG(t *testing.T) {
	keyring, _ := NewBLSKeyring(1, 4)
	if err := DumpBLSKeyring(keyring, "/Users/andrei/tmp"); err != nil {
		panic(err)
	}
}

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
