package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tendermint/tendermint/proxy"

	cmd "github.com/tendermint/tendermint/cmd/tendermint/commands"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/cli"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	bls "github.com/tendermint/tendermint/node/bls"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
)

func main() {
	rootCmd := cmd.RootCmd
	rootCmd.AddCommand(
		cmd.GenValidatorCmd,
		cmd.InitFilesCmd,
		cmd.ProbeUpnpCmd,
		cmd.LiteCmd,
		cmd.ReplayCmd,
		cmd.ReplayConsoleCmd,
		cmd.ResetAllCmd,
		cmd.ResetPrivValidatorCmd,
		cmd.ShowValidatorCmd,
		cmd.TestnetFilesCmd,
		cmd.ShowNodeIDCmd,
		cmd.GenNodeKeyCmd,
		cmd.VersionCmd)

	// NOTE:
	// Users wishing to:
	//	* Use an external signer for their validators
	//	* Supply an in-proc abci app
	//	* Supply a genesis doc file from another source
	//	* Provide their own DB implementation
	// can copy this file and use something other than the
	// DefaultNewNode function
	nodeFunc := NewBLSNode

	// Create & start node
	rootCmd.AddCommand(cmd.NewRunBLSNodeCmd(nodeFunc))

	cmd := cli.PrepareBaseCmd(rootCmd, "TM", os.ExpandEnv(filepath.Join("$HOME", cfg.DefaultTendermintDir)))
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}

func NewBLSNode(config *cfg.Config, logger log.Logger) (*bls.BLSNode, error) {

	// Generate node PrivKey

	nodeKey, err := p2p.LoadOrGenNodeKey(config.NodeKeyFile())
	if err != nil {

		return nil, err

	}

	// Convert old PrivValidator if it exists.
	oldPrivVal := config.OldPrivValidatorFile()
	newPrivValKey := config.PrivValidatorKeyFile()
	newPrivValState := config.PrivValidatorStateFile()
	if _, err := os.Stat(oldPrivVal); !os.IsNotExist(err) {
		oldPV, err := privval.LoadOldFilePV(oldPrivVal)
		if err != nil {

			return nil, fmt.Errorf("error reading OldPrivValidator from %v: %v\n", oldPrivVal, err)

		}
		logger.Info("Upgrading PrivValidator file",
			"old", oldPrivVal,
			"newKey", newPrivValKey,
			"newState", newPrivValState,
		)

		oldPV.Upgrade(newPrivValKey, newPrivValState)
	}

	//cc := proxy.NewLocalClientCreator(app)
	cc := proxy.DefaultClientCreator(config.ProxyApp, config.ABCI, config.DBDir())

	blockStore, stateDB, bcReactor, consensusReactor, consensusState, err := bls.GetBLSReactors(
		config,
		privval.LoadOrGenFilePV(newPrivValKey, newPrivValState),
		node.DefaultMetricsProvider(config.Instrumentation),
		logger,
		cc,
	)
	if err != nil {
		panic(err)
	}

	return bls.NewBLSNode(config,
		privval.LoadOrGenFilePV(newPrivValKey, newPrivValState),
		nodeKey,
		cc,
		node.DefaultGenesisDocProviderFunc(config),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(config.Instrumentation),
		logger,
		blockStore,
		stateDB,
		bls.CustomBLSReactors(map[string]p2p.Reactor{
			"BLOCKCHAIN": bcReactor,
			"CONSENSUS":  consensusReactor,
		}),
		bls.CustomBLSConsensusState(consensusState),
	)
}
