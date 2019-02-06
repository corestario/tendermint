package main

import (
	"flag"
	"fmt"
	"os"
	"path"
	"runtime"
	"strconv"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/types/time"
	tmtime "github.com/tendermint/tendermint/types/time"
)

type node struct {
	Key *p2p.NodeKey
	PV  *privval.FilePV
}

func main() {
	n := flag.Int("N", 4, "number of live nodes")
	deadN := flag.Int("d", 0, "number of dead nodes")
	flag.Parse()

	_, fl, _, _ := runtime.Caller(0)
	remoteDir := path.Dir(fl) + "/.."

	nodes := make([]node, *n)
	path := remoteDir + "/nodes/list/"

	if *n < 4 {
		fmt.Println("N should be more or equal 4")
		os.Exit(0)
	}

	for i := 0; i < *n; i++ {
		cfgPath := path + "node" + strconv.Itoa(i) + "/"
		err := os.Mkdir(cfgPath, os.ModePerm)
		if err != nil {
			panic(err)
		}

		cfgPath = cfgPath + "config/"
		err = os.Mkdir(cfgPath, os.ModePerm)
		if err != nil {
			panic(err)
		}

		key, err := p2p.LoadOrGenNodeKey(cfgPath + "node_key.json")
		if err != nil {
			panic(err)
		}

		pv := privval.GenFilePV(cfgPath+"priv_validator.json", cfgPath+"state_file")
		pv.Save()
		nodes[i] = node{
			Key: key,
			PV:  pv,
		}
		err = os.Link(remoteDir+"/nodes/config.toml", cfgPath+"config.toml")
		if err != nil {
			panic(err)
		}

	}
	genDoc := types.GenesisDoc{
		ChainID:     fmt.Sprintf("test-chain-%v", time.Now().Unix()),
		GenesisTime: tmtime.Now(),
	}
	genDoc.Validators = make([]types.GenesisValidator, *n)

	for i := 0; i < *n; i++ {
		genDoc.Validators[i] = types.GenesisValidator{
			Address: nodes[i].PV.GetPubKey().Address(),
			PubKey:  nodes[i].PV.GetPubKey(),
			Power:   10,
		}
	}

	for i := *n; i < *n+*deadN; i++ {
		pv := ed25519.GenPrivKey()

		genDoc.Validators[i] = types.GenesisValidator{
			Address: pv.PubKey().Address(),
			PubKey:  pv.PubKey(),
			Power:   10,
		}
	}

	for i := 0; i < *n; i++ {
		genesisPath := path + "node" + strconv.Itoa(i) + "/config/genesis.json"
		if err := genDoc.SaveAs(genesisPath); err != nil {
			fmt.Println("Can't save Genesis", err)
		}
	}
}
