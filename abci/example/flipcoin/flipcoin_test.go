package flipcoin

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/abci/example/code"
	"github.com/tendermint/tendermint/abci/types"
)

func TestCheckTx_ValidTxGiven_Success(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	tx := []byte("user1=Tail")

	// act
	res := app.CheckTx(tx)

	// assert
	require.True(t, res.IsOK(), "CheckTx failed: %s", res.Log)
}

func TestCheckTx_InvalidTxGiven_ErrorReturned(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	tx := []byte("InvalidTx")

	// act
	res := app.CheckTx(tx)

	// assert
	require.Equal(t, code.CodeInvalidTransaction, res.Code, "CheckTx failed: %s", res.Log)
}

func TestDeliverTx_ValidTxGiven_Success(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	tx := []byte("user1=Head")

	// act
	res := app.DeliverTx(tx)

	// assert
	require.True(t, res.IsOK(), "DeliverTx failed: %s", res.Log)
}

func TestDeliverTx_InvalidTxGiven_ErrorReturned(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	tx := []byte("InvalidTxFormat")

	// act
	res := app.DeliverTx(tx)

	// assert
	require.Equal(t, code.CodeInvalidTransaction, res.Code, "DeliverTx failed: %s", res.Log)
}

func TestDeliverTx_UnknownSideGiven_ErrorReturned(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	tx := []byte("user1=InvalidSide")

	// act
	res := app.DeliverTx(tx)

	// assert
	require.Equal(t, code.CodeTypeUnknownSide, res.Code, "DeliverTx failed: %s", res.Log)
}

func TestInfo(t *testing.T) {
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	flipcoin := NewFlipCoinApplication(dir)

	height := int64(0)

	resInfo := flipcoin.Info(types.RequestInfo{})
	require.Equal(t, height, resInfo.LastBlockHeight,
		"expected height of %d, got %d", height, resInfo.LastBlockHeight)

	height = int64(1)
	header := types.Header{Height: int64(height)}

	flipcoin.BeginBlock(types.RequestBeginBlock{Hash: []byte("foo"), Header: header})
	flipcoin.EndBlock(types.RequestEndBlock{Height: header.Height})
	flipcoin.Commit()

	resInfo = flipcoin.Info(types.RequestInfo{})
	require.Equal(t, height, resInfo.LastBlockHeight,
		"expected height of %d, got %d", height, resInfo.LastBlockHeight)
}

func TestQuery_InvalidTxGiven_ErrorReturned(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	resQuery := types.RequestQuery{
		Data: []byte("InvalidTxFormat"),
	}

	// act
	res := app.Query(resQuery)

	// assert
	require.Equal(t, code.CodeInvalidTransaction, res.Code, "Query failed: %s", res.Log)
}

func TestQuery_NoGamePlayed_Success(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	resQuery := types.RequestQuery{
		Data: []byte("user=1"),
	}

	// act
	res := app.Query(resQuery)

	// assert
	require.True(t, res.IsOK())
	require.Containsf(t, res.Log, "No game found", "Query failed: %s", res.Log)
}

func TestQuery_GameIsWon_Success(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	app.DeliverTx([]byte("user=Head"))
	app.BeginBlock(types.RequestBeginBlock{Header: types.Header{RandomNumber: 42}}) // just an even number to win the game
	app.Commit()

	resQuery := types.RequestQuery{
		Data: []byte("user=1"),
	}

	// act
	res := app.Query(resQuery)

	// assert
	require.True(t, res.IsOK())
	require.Containsf(t, res.Log, "You won", "Query failed: %s", res.Log)
}

func TestQuery_GameIsLost_Success(t *testing.T) {
	// arrange
	dir := createTestDir(t)
	defer cleanUp(t, dir)

	app := NewFlipCoinApplication(dir)
	app.DeliverTx([]byte("user=Tail"))
	app.BeginBlock(types.RequestBeginBlock{Header: types.Header{RandomNumber: 42}}) // just an even number to lose the game
	app.Commit()

	resQuery := types.RequestQuery{
		Data: []byte("user=1"),
	}

	// act
	res := app.Query(resQuery)

	// assert
	require.True(t, res.IsOK())
	require.Containsf(t, res.Log, "You lost", "Query failed: %s", res.Log)
}

func createTestDir(t *testing.T) string {
	dir, err := ioutil.TempDir("/tmp", "abci-flipcoin-test")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func cleanUp(t *testing.T, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
}
