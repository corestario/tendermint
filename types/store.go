package types

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"

	"go.dedis.ch/kyber"

	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"

	"go.dedis.ch/kyber/pairing/bn256"
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
	MPubKeyCommits string `json:"musterPubKey"` // it's parts of MasterPubKey (type point)
	PubShare       string `json:"pubShare"`
	PrivShare      string `json:"privShare"`
}

func NewBLSKeyJSON(key *BLSKey) (*BLSKeyJSON, error) {
	masterKeyBuf := bytes.NewBuffer(nil)
	masterKeyEnc := gob.NewEncoder(masterKeyBuf)
	_, commits := key.MasterPubKey.Info()
	if err := masterKeyEnc.Encode(commits); err != nil {
		return nil, fmt.Errorf("failed to encode master public key commits: %v", err)
	}

	shareJSON, err := NewBLSShareJSON(key.Share)
	if err != nil {
		return nil, fmt.Errorf("failed to encode key share: %v", err)
	}

	return &BLSKeyJSON{
		N:              key.N,
		MPubKeyCommits: base64.StdEncoding.EncodeToString(masterKeyBuf.Bytes()),
		PubShare:       shareJSON.Pub,
		PrivShare:      shareJSON.Priv,
	}, nil
}

func (blsJSON *BLSKeyJSON) Deserialize() (*BLSKey, error) {
	masterPubBytes, err := base64.StdEncoding.DecodeString(blsJSON.MPubKeyCommits)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode commits of masterPubKey: %v", err)
	}
	mPubCommitsDec := gob.NewDecoder(bytes.NewBuffer(masterPubBytes))
	mPubKeyCommits := make([]kyber.Point, blsJSON.N)
	g2 := bn256.NewSuite().G2()
	for i := 0; i < blsJSON.N; i++ {
		mPubKeyCommits[i] = g2.Point()
	}
	if err := mPubCommitsDec.Decode(&mPubKeyCommits); err != nil {
		return nil, fmt.Errorf("failed to decode commits of masterPubKey: %v", err)
	}
	mPubKey := share.NewPubPoly(g2, nil, mPubKeyCommits)
	shareJSON := &BLSShareJSON{blsJSON.PubShare, blsJSON.PrivShare}
	share, err := shareJSON.Deserialize()
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize BLSshare: %v", err)
	}
	return &BLSKey{
		N:            blsJSON.N,
		MasterPubKey: mPubKey,
		Share:        share,
	}, nil
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

func calcEpochKey(epoch int64) []byte {
	return []byte(fmt.Sprintf("EP:%v", epoch))
}

// Load BLSkey from KeyStore
func (ks *KeyStore) GetBLSKey(epoch int64) *BLSKey {
	bz := ks.db.Get(calcEpochKey(epoch))
	if len(bz) == 0 {
		return nil
	}
	blskeyJSON := &BLSKeyJSON{}
	err := cdc.UnmarshalBinaryBare(bz, blskeyJSON)
	if err != nil {
		panic(fmt.Sprintf("Could not unmarshal bytes: %X", bz))
	}
	blsKey, err := blskeyJSON.Deserialize()
	if err != nil {
		panic(fmt.Sprintf("failed to deserialize blskey: %X", err))
	}
	return blsKey
}

// Save BLSkey to KeyStore
func (ks *KeyStore) SetBLSKey(blsKey *BLSKey, epoch int64) error {
	blsKeysJSON, err := NewBLSKeyJSON(blsKey)
	if err != nil {
		panic(fmt.Sprintf("failed to serialize blskey: %X", err))
	}
	bz, err := cdc.MarshalBinaryBare(blsKeysJSON)
	if err != nil {
		panic(fmt.Sprintf("Could not marshal bytes: %X", bz))
	}
	ks.db.SetSync(calcEpochKey(epoch), bz)
	return nil
}
