package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/template"

	"github.com/corestario/dkglib/lib/blsShare"
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

func InitFilesWithConfig(config *cfg.Config) error {
	return initFilesWithConfig(config)
}

func initFilesWithConfig(config *cfg.Config) error {
	// private validator
	privValKeyFile := config.PrivValidatorKeyFile()
	privValStateFile := config.PrivValidatorStateFile()
	var pv *privval.FilePV
	if cmn.FileExists(privValKeyFile) {
		pv = privval.LoadFilePV(privValKeyFile, privValStateFile)
		logger.Info("Found private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	} else {
		pv = privval.GenFilePV(privValKeyFile, privValStateFile)
		pv.Save()
		logger.Info("Generated private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
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

	blsKeyring, err := blsShare.NewBLSKeyring(config.DKGOnChainConfig.BLSThreshold, config.DKGOnChainConfig.BLSNumShares)
	if err != nil {
		return fmt.Errorf("failed to run NewBLSKeyring: %w", err)
	}

	blsKeyFile := config.BLSKeyFile()
	if cmn.FileExists(blsKeyFile) {
		logger.Info("Found node key", "path", blsKeyFile)
	} else if !withoutGeneratedBLSKeys {
		f, err := os.Create(blsKeyFile)
		if err != nil {
			return err
		}
		defer f.Close()

		share, ok := blsKeyring.Shares[config.NodeID]
		if !ok {
			return fmt.Errorf("node id #%d is unexpected", config.NodeID)
		}

		shareJSON, err := blsShare.NewBLSShareJSON(share)
		if err != nil {
			return fmt.Errorf("failed to load LoadBLSShareJSON: %w", err)
		}

		err = json.NewEncoder(f).Encode(shareJSON)
		if err != nil {
			return err
		}

		logger.Info("Generated node key", "path", blsKeyFile)
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
		key := pv.GetPubKey()
		genDoc.Validators = []types.GenesisValidator{{
			Address: key.Address(),
			PubKey:  key,
			Power:   10,
		}}

		// This keypair allows for single-node execution, e.g. $ tendermint node.
		genDoc.BLSMasterPubKey = blsShare.DefaultBLSVerifierMasterPubKey
		genDoc.BLSThreshold = 2
		genDoc.BLSNumShares = 4
		genDoc.DKGNumBlocks = 1000

		if err := genDoc.SaveAs(genFile); err != nil {
			return err
		}
		logger.Info("Generated genesis file", "path", genFile)
	}

	return nil
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

type Nodes struct {
	Nodes []Node
}

type Node struct {
	StartPort int
	EndPort   int
	IP        int
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
    command: node --proxy_app=kvstore --log_level=debug
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
