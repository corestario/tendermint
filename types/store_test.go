package types

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"

	dbm "github.com/tendermint/tm-db"
	"go.dedis.ch/kyber/pairing/bn256"
)

func TestBLSKeySerialization(t *testing.T) {
	pubPoly, _ := LoadPubKey(TestnetMasterPubKey, 4)

	for _, tc := range TestnetShares {
		share, _ := tc.Deserialize()

		key := &BLSKey{
			N:            4,
			MasterPubKey: pubPoly,
			Share:        share,
		}

		keyJSON, err := NewBLSKeyJSON(key)
		if err != nil {
			t.Errorf("failed to create BLSKeyJSON object: %v", err)
			return
		}

		key2, err2 := keyJSON.Deserialize()
		if err2 != nil {
			t.Errorf("failed to deserialize BLSKeyJSON object: %v", err2)
			return
		}

		if !key.IsEqual(key2) {
			t.Errorf("Object before the serialization and object after the seriailization are not equal")
		}
	}
}

func (key1 *BLSKey) IsEqual(key2 *BLSKey) bool {
	if key1.N != key2.N ||
		!key1.MasterPubKey.Equal(key2.MasterPubKey) ||
		!key1.Share.IsEqual(key2.Share) {
		return false
	}
	return true
}

func (share1 *BLSShare) IsEqual(share2 *BLSShare) bool {
	//Kyber doesn't allow comparison between two Priv/PubShare in a "simple" way, so we use hash instead
	suite := bn256.NewSuite()
	pubHash1 := share1.Pub.Hash(suite)
	pubHash2 := share2.Pub.Hash(suite)
	privHash1 := share1.Priv.Hash(suite)
	privHash2 := share2.Priv.Hash(suite)
	if share1.ID != share2.ID ||
		!bytes.Equal(pubHash1, pubHash2) ||
		!bytes.Equal(privHash1, privHash2) {
		return false
	}
	return true
}

func TestSaveLoadBLSKey(t *testing.T) {
	//set keyStore
	backend := "leveldb"
	dirname, err := ioutil.TempDir("", fmt.Sprintf("test_keystore_%s_", backend))
	if err != nil {
		t.Errorf("Temporary directory wasn't created")
	}
	dbType := dbm.DBBackendType(backend)
	keyStoreDB := dbm.NewDB("keyStore", dbType, dirname)
	keyStore := NewKeyStore(keyStoreDB)

	//set BLSKey for saving to the keyStore
	pubPoly, _ := LoadPubKey(TestnetMasterPubKey, 4)

	for i, tc := range TestnetShares {
		share, _ := tc.Deserialize()

		key := &BLSKey{
			N:            4,
			MasterPubKey: pubPoly,
			Share:        share,
		}
		keyStore.SetBLSKey(key, int64(i))
		loadedKey := keyStore.GetBLSKey(int64(i))

		if !key.IsEqual(loadedKey) {
			t.Errorf("Saved and loaded keys are not equal")
		}
	}
}
