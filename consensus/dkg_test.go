package consensus

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"go.dedis.ch/kyber/pairing/bn256"
)

func createDKGMsg(addr []byte, roundID int, data []byte, toIndex, numEntities int) DKGDataMessage {
	return DKGDataMessage{
		&types.DKGData{
			Type:        types.DKGDeal,
			Addr:        addr,
			RoundID:     roundID,
			Data:        data,
			ToIndex:     toIndex,
			NumEntities: numEntities,
		},
	}
}

//TestDKGDataSignable test
func TestDKGDataSignable(t *testing.T) {
	var (
		expected, signBytes []byte
		err                 error
	)
	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)

	signBytes = msg.Data.SignBytes()

	msg.Data.Signature = nil
	if expected, err = cdc.MarshalBinaryLengthPrefixed(msg.Data); err != nil {
		t.Error(err.Error())
		return
	}
	require.Equal(t, expected, signBytes, "Got unexpected sign bytes for DKGData.")
}

func TestDKGPKStoreFindByAddress(t *testing.T) {
	var (
		pkStore PKStore
	)
	N := 1000
	testKeys := make([]PK2Addr, 0, N)
	for i := 0; i < N; i++ {
		suite := bn256.NewSuiteG2()
		testKeys = append(testKeys, PK2Addr{crypto.Address([]byte{byte(i)}), suite.Point().Pick(suite.RandomStream())})
		pkStore.Add(&testKeys[i])
	}

	sort.Sort(pkStore)

	for _, v := range testKeys {
		found, err := pkStore.FindByAddress(v.Addr.String())
		require.NoError(t, nil, err)
		require.Equal(t, v.Addr, found.Addr)
	}
}

func TestDKGVerifyMessage(t *testing.T) {
	privVal := types.NewMockPV()
	//pubKey for dealer's address
	pubkey := privVal.GetPubKey()

	dealer := NewDKGDealer(nil, pubkey, nil, log.NewNopLogger())

	//key pair for sign/verify messages
	dealer.secKey = dealer.suiteG2.Scalar().Pick(dealer.suiteG2.RandomStream())
	dealer.pubKey = dealer.suiteG2.Point().Mul(dealer.secKey, nil)

	//let's pretend another peer's dealer gets pubKey and saves it
	dealer.pubKeys.Add(&PK2Addr{crypto.Address(pubkey.Address().Bytes()), dealer.pubKey})

	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)

	require.NoError(t, nil, dealer.Sign(msg.Data))

	require.NoError(t, nil, dealer.VerifyMessage(msg))
}
