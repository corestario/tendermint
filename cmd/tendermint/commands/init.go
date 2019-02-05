package commands

import (
	"fmt"
	"html/template"
	"os"

	"github.com/tendermint/tendermint/dgaming-crypto/go/bls"

	"github.com/spf13/cobra"

	cfg "github.com/tendermint/tendermint/config"
	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

// InitFilesCmd initialises a fresh Tendermint Core instance.
var InitFilesCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Tendermint",
	RunE:  initFiles,
}

func initFiles(cmd *cobra.Command, args []string) error {
	return initFilesWithConfig(config)
}

func initFilesWithConfig(config *cfg.Config) error {
	// private validator
	privValFile := config.PrivValidatorFile()
	var pv *privval.FilePV
	if cmn.FileExists(privValFile) {
		pv = privval.LoadFilePV(privValFile)
		logger.Info("Found private validator", "path", privValFile)
	} else {
		pv = privval.GenFilePV(privValFile)
		pv.Save()
		logger.Info("Generated private validator", "path", privValFile)
	}

	nodeKeyFile := config.NodeKeyFile()
	if cmn.FileExists(nodeKeyFile) {
		logger.Info("Found node key", "path", nodeKeyFile)
	} else {
		if _, err := p2p.LoadOrGenNodeKey(nodeKeyFile); err != nil {
			return err
		}
		logger.Info("Generated node key", "path", nodeKeyFile)
	}

	// genesis file
	genFile := config.GenesisFile()
	if cmn.FileExists(genFile) {
		logger.Info("Found genesis file", "path", genFile)
	} else {
		genDoc := types.GenesisDoc{
			ChainID:         fmt.Sprintf("test-chain-%v", cmn.RandStr(6)),
			GenesisTime:     tmtime.Now(),
			ConsensusParams: types.DefaultConsensusParams(),
		}
		genDoc.Validators = []types.GenesisValidator{{
			Address: pv.GetPubKey().Address(),
			PubKey:  pv.GetPubKey(),
			Power:   10,
		}}

		// This keypair allows for single-node execution, e.g. `$ tendermint node`.
		genDoc.BLSMasterPubKey = types.SolitaireBLSVerifierMasterPubKey
		genDoc.BLSKeypair = &bls.SerializedKeypair{
			Id:   types.SolitaireBLSVerifierID,
			Pub:  types.SolitaireBLSVerifierPubKey,
			Priv: types.SolitaireBLSVerifierPrivKey,
		}
		genDoc.Others = map[string]*bls.SerializedKeypair{
			pv.GetPubKey().Address().String(): {
				Id:  types.SolitaireBLSVerifierID,
				Pub: types.SolitaireBLSVerifierPubKey,
			},
		}

		if err := genDoc.SaveAs(genFile); err != nil {
			return err
		}
		logger.Info("Generated genesis file", "path", genFile)
	}

	return nil
}

type Nodes struct {
	Nodes []Node
}

type Node struct {
	StartPort int
	EndPort   int
	IP        int
}

func writeDockerCompose(nValidators int, p2pPort int) error {
	startIP := 2

	nodes := make([]Node, nValidators)
	for i := range nodes {
		nodes[i] = Node{
			StartPort: p2pPort + 2*i,
			EndPort:   p2pPort + 2*i + 1,
			IP:        startIP + i,
		}
	}

	composeTmpl := template.Must(template.New("docker-compose").Parse(templ))

	f, err := os.Create("docker-compose.yml")
	if err != nil {
		return err
	}

	return composeTmpl.Execute(f, Nodes{nodes})
}

const templ = `version: '3'

services:{{range $i, $e := .Nodes}}
  node{{$i}}:
    container_name: node{{$i}}
    image: "tendermint/localnode"
    ports:
      - "{{.StartPort}}-{{.EndPort}}:26656-26657"
    environment:
      - ID={{$i}}
      - LOG=tendermint.log
    volumes:
      - ./build:/tendermint:Z
    networks:
      localnet:
        ipv4_address: 192.167.10.{{.IP}}
{{end}}
networks:
  localnet:
    driver: bridge
    ipam:
      driver: default
      config:
      -
        subnet: 192.167.10.0/16

`
