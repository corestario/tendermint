package main

import (
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/p2p"
	tmtime "github.com/tendermint/tendermint/types/time"

	"fmt"
	"flag"
	"os"
	"strconv"
	"github.com/tendermint/tendermint/types"
	"github.com/tendermint/tendermint/types/time"
)

type node struct {
	Key *p2p.NodeKey
	PV  *privval.FilePV
}

func main() {
	n := flag.Int("N", 4, "num of nodes")
	flag.Parse()
	arr := make([]node, *n)
	path := "/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/nodes/list/"

	if *n < 4 {
		fmt.Println("N should be more 4")
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
		_ = key

		pv := privval.GenFilePV(cfgPath + "priv_validator.json")
		pv.Save()
		arr[i] = node{
			Key: key,
			PV:  pv,
		}
		err = os.Link("/Users/boris/go/src/github.com/tendermint/tendermint/networks/remote/nodes/config.toml", cfgPath+"config.toml")
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
			Address: arr[i].PV.GetPubKey().Address(),
			PubKey:  arr[i].PV.GetPubKey(),
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
