package flipcoin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/tendermint/tendermint/abci/example/code"
	"github.com/tendermint/tendermint/abci/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/version"
)

var (
	stateKey        = []byte("stateKey")
	statusPrefixKey = []byte("S=")

	ProtocolVersion version.Protocol = 0x1
)

type State struct {
	db           dbm.DB
	randomNumber int64

	TxCount int64  `json:"txs"`
	Height  int64  `json:"height"`
	AppHash []byte `json:"app_hash"`
}

type Status []byte

var (
	Win  = Status("Win")
	Lost = Status("Lost")
)

type Side []byte

func sideFromBytes(side []byte) (Side, error) {
	if bytes.Equal(side, Head) {
		return Head, nil
	}
	if bytes.Equal(side, Tail) {
		return Tail, nil
	}
	return nil, fmt.Errorf("unknown side: %s", string(side))
}

var (
	Head = Side("Head")
	Tail = Side("Tail")
)

func loadState(db dbm.DB) State {
	stateBytes := db.Get(stateKey)
	var state State
	if len(stateBytes) != 0 {
		err := json.Unmarshal(stateBytes, &state)
		if err != nil {
			panic(err)
		}
	}
	state.db = db
	return state
}

func saveState(state State) {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	state.db.Set(stateKey, stateBytes)
}

func prefixKey(userID []byte, blockHeight int64) []byte {
	height := make([]byte, 8)
	binary.LittleEndian.PutUint64(height, uint64(blockHeight))

	return []byte(fmt.Sprintf("%v%v:%v", statusPrefixKey, userID, height))
}

//---------------------------------------------------

type Application struct {
	types.BaseApplication

	state State
}

func NewFlipCoinApplication(dbDir string) *Application {
	db, err := dbm.NewGoLevelDB("flipcoin", dbDir)
	if err != nil {
		panic(err)
	}
	//db := dbm.NewMemDB()
	state := loadState(db)
	return &Application{state: state}
}

func (app *Application) Info(req types.RequestInfo) (resInfo types.ResponseInfo) {
	return types.ResponseInfo{
		Data:             fmt.Sprintf("{\"height\":%v,\"txs\":%v}", app.state.Height, app.state.TxCount),
		Version:          version.ABCIVersion,
		AppVersion:       ProtocolVersion.Uint64(),
		LastBlockAppHash: app.state.AppHash,
		LastBlockHeight:  app.state.Height,
	}
}

func (app *Application) CheckTx(tx []byte) types.ResponseCheckTx {
	_, _, err := app.parseFlipCoinTransaction(tx)
	if err != nil {
		return types.ResponseCheckTx{
			Code: code.CodeInvalidTransaction,
			Log:  fmt.Sprintf("check tx error: %v", err),
		}
	}
	return types.ResponseCheckTx{Code: code.CodeTypeOK, GasWanted: 1}
}

func (app *Application) parseFlipCoinTransaction(tx []byte) ([]byte, []byte, error) {
	parts := bytes.Split(tx, []byte("="))
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("invalid tx format, expected user_id=side, got %s", string(tx))
	}

	return parts[0], parts[1], nil
}

func (app *Application) BeginBlock(req types.RequestBeginBlock) types.ResponseBeginBlock {
	app.state.randomNumber = req.Header.RandomNumber

	return types.ResponseBeginBlock{}
}

func (app *Application) DeliverTx(tx []byte) types.ResponseDeliverTx {
	userID, sideBytes, err := app.parseFlipCoinTransaction(tx)
	if err != nil {
		return types.ResponseDeliverTx{
			Code: code.CodeInvalidTransaction,
			Log:  fmt.Sprintf("deliver tx error: %v", err),
		}
	}

	playerSide, err := sideFromBytes(sideBytes)
	if err != nil {
		return types.ResponseDeliverTx{
			Code: code.CodeTypeUnknownError,
			Log:  fmt.Sprintf("Unknown side given %s", string(sideBytes)),
		}
	}

	randSide, err := app.flipCoin()
	if err != nil {
		return types.ResponseDeliverTx{
			Code: code.CodeRandomUnavailableError,
			Log:  fmt.Sprintf("Random number is not available"),
		}
	}

	status := Lost
	if bytes.Equal(playerSide, randSide) {
		status = Win
	}

	key := prefixKey(userID, app.state.Height+1)

	app.state.db.Set(key, status)
	app.state.TxCount += 1

	tags := []cmn.KVPair{
		{Key: []byte("app.key"), Value: key},
		{Key: []byte("app.status"), Value: status},
	}

	return types.ResponseDeliverTx{
		Code: code.CodeTypeOK,
		Log:  fmt.Sprintf("Game status: %s, Random: %d", string(status), app.state.randomNumber),
		Tags: tags,
	}
}

func (app *Application) flipCoin() (Side, error) {
	if app.state.randomNumber%2 == 0 {
		return Head, nil
	}

	return Tail, nil
}

func (app *Application) Commit() types.ResponseCommit {
	app.state.Height++

	if app.state.TxCount == 0 {
		return types.ResponseCommit{}
	}

	appHash := make([]byte, 8)
	binary.PutVarint(appHash, app.state.TxCount)

	app.state.AppHash = appHash
	saveState(app.state)

	return types.ResponseCommit{Data: appHash}
}

func (app *Application) Query(reqQuery types.RequestQuery) (resQuery types.ResponseQuery) {
	userID, blockHeight, err := app.parseFlipCoinQuery(reqQuery.Data)
	if err != nil {
		return types.ResponseQuery{
			Code: code.CodeInvalidTransaction,
			Log:  fmt.Sprintf("query error: %v", err),
		}
	}

	statusBytes := app.state.db.Get(prefixKey(userID, blockHeight))
	if statusBytes == nil {
		resQuery.Log = "No game found"
	} else if bytes.Equal(statusBytes, Win) {
		resQuery.Log = "You won"
	} else {
		resQuery.Log = "You lost"
	}
	resQuery.Value = statusBytes

	return
}

func (app *Application) parseFlipCoinQuery(tx []byte) ([]byte, int64, error) {
	parts := bytes.Split(bytes.TrimSpace(tx), []byte("="))
	if len(parts) != 2 {
		return nil, 0, fmt.Errorf("invalid query format, expected user_id=height, got %s", string(tx))
	}

	blockHeight, err := strconv.ParseInt(string(parts[1]), 10, 64)
	return parts[0], blockHeight, err
}
