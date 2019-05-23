package types

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"

	"go.dedis.ch/kyber"

	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"

	"go.dedis.ch/kyber/share"
)

//KeyStore is store for dkg-results(BLS-keys)
type KeyStore struct {
	db           dbm.DB
	currentEpoch int64 //pointer to the current epoch (last valid keys)
}

var keyStoreKey = []byte("keyStore")

type KeyStoreStateJSON struct {
	CurrentEpoch int64 `json:"currentEpoch"`
}

type BLSKey struct {
	N            int
	MasterPubKey *share.PubPoly // Public key used to verify individual and aggregate signatures
	Share        *BLSShare      // Public + private shares
}

type BLSKeyJSON struct {
	N              int    `json:"n"`
	MPubKeyCommits string `json:"musterPubKey"`
	PubShare       string `json:"pubShare"`
	PrivShare      string `json:"privShare"`
}

func NewKeySetJSON(keySet KeySet) (*KeySetJSON, error) {
	masterKeyBuf := bytes.NewBuffer(nil)
	masterKeyEnc := gob.NewEncoder(masterKeyBuf)
	if err := masterKeyEnc.Encode(keySet.MasterPubKey); err != nil {
		return nil, fmt.Errorf("failed to encode master public key: %v", err)
	}

	sharesBuf := bytes.NewBuffer(nil)
	sharesEnc := gob.NewEncoder(sharesBuf)
	if err := sharesEnc.Encode(keySet.KeyShares); err != nil {
		return nil, fmt.Errorf("failed to encode public key shares: %v", err)
	}

	return &KeySetJSON{
		MasterPubKey: base64.StdEncoding.EncodeToString(masterKeyBuf.Bytes()),
		KeyShares:    base64.StdEncoding.EncodeToString(sharesBuf.Bytes()),
	}, nil
}

func (ksJSON *KeySetJSON) Deserialize() (*KeySet, error) {
	bytes := ksJSON.N
	var N int
	err := cdc.UnmarshalJSON(bytes, &N)
	if err != nil {
		panic(fmt.Sprintf("Could not unmarshal bytes: %X", bytes))
	}

	masterPubBytes, err := base64.StdEncoding.DecodeString(ksJSON.MPubKeyCommits)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode commits of masterPubKey: %v", err)
	}
	MPubCommitsDec := gob.NewDecoder(bytes.NewBuffer(masterPubBytes))
	MPubKeyCommits := make([]kyber.Point, N)
	if err := masterPubDec.Decode(masterPubKey); err != nil {
		return nil, fmt.Errorf("failed to decode masterPubKey: %v", err)
	}
}

//Save persists the keyStore state (current epoch) to the database as JSON
func (ksj KeyStoreStateJSON) Save(db dbm.DB) {
	bytes, err := cdc.MarshalJSON(ksj)
	if err != nil {
		cmn.PanicSanity(fmt.Sprintf("Could not marshal state bytes: %v", err))
	}
	db.SetSync(keyStoreKey, bytes)
}

//LoadKeyStoreStateJSON returns the KeyStoreStateJSON (current epoch as JSON)
func LoadKeyStoreStateJSON(db dbm.DB) KeyStoreStateJSON {
	bytes := db.Get(keyStoreKey)

	if len(bytes) == 0 {
		return KeyStoreStateJSON{
			CurrentEpoch: 0,
		}
	}

	ksj := KeyStoreStateJSON{}
	err := cdc.UnmarshalJSON(bytes, &ksj)
	if err != nil {
		panic(fmt.Sprintf("Could not unmarshal bytes: %X", bytes))
	}

	return ksj
}

// NewKeyStore returns new KeyStore with the given DB
func NewKeyStore(db dbm.DB) *KeyStore {
	ksJSON := LoadKeyStoreStateJSON(db)
	return &KeyStore{
		db:           db,
		currentEpoch: ksJSON.CurrentEpoch,
	}
}
