package consensus

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
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

// TestDKGDataSignable test
func TestDKGDataSignable(t *testing.T) {
	var (
		expected, signBytes []byte
		err                 error
	)
	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)

	if signBytes, err = msg.Data.SignBytes(); err != nil {
		t.Error(err.Error())
		return
	}

	msg.Data.Signature = nil
	if expected, err = cdc.MarshalBinaryLengthPrefixed(msg.Data); err != nil {
		t.Error(err.Error())
		return
	}
	require.Equal(t, expected, signBytes, "Got unexpected sign bytes for DKGData.")
}

func TestDKGVerifyMessage(t *testing.T) {
	privVal := types.NewMockPV()

	// pubKey for dealer's address
	pubkey := privVal.GetPubKey()

	validator := types.NewValidator(pubkey, 10)
	validators := types.NewValidatorSet([]*types.Validator{validator})

	dealer := NewDKGDealer(validators, privVal, nil, log.NewNopLogger())

	// key pair for sign/verify messages
	dealer.secKey = dealer.suiteG2.Scalar().Pick(dealer.suiteG2.RandomStream())
	dealer.pubKey = dealer.suiteG2.Point().Mul(dealer.secKey, nil)

	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)

	require.NoError(t, nil, dealer.Sign(msg.Data))

	require.NoError(t, nil, dealer.VerifyMessage(msg))
}
