### Demo scenarios for Corestar-ICF Acrade Grant

##### Recap

In 2019 Corestar received an ICF grant for an implementation of random data generation inside Tendermint consensus. This document describes the scenarios that aim to demonstrate current progress and state of development.

##### Scenario No. 1: Run with pre-generated BLS keys

Arcade's random data generation is based on threshold BLS signature recovery, where the recovered signature is put into block header. For a network of nodes to run, we can put pre-generated BLS keys into each node's data directory and run the testnet. This is currently done automatically, and the procedure is not different from vanilla local testnet initialization:

```
cd randapp
rm -rf ./build
make build-docker-rdnode
make build-linux
make localnet-stop
make localnet-start
```    

Expected output of `docker logs -f $(docker ps -a -q)`:

```
node1    | I[2020-02-11|12:59:21.001] Received proposal                            module=consensus proposal="Proposal{2/0 (CE466AC15102556F98120CEB5B67D1D8F4ACE7E1ED4C71691B37BF5F2F93CC56:1:2FB758004ECE, -1) 00ED90556306 @ 2020-02-11T12:59:20.8977317Z}"
node1    | I[2020-02-11|12:59:21.001] Added to prevote                             module=consensus vote="Vote{1:5FC25D52B58F 2/00/1(Prevote) CE466AC15102 EC9748C58162 @ 2020-02-11T12:59:20.9022709Z BLSSignature: []}" prevotes="VoteSet{H:2 R:0 T:1 +2/3:<nil>(0.25) BA{4:_x__} map[]}"
node1    | I[2020-02-11|12:59:21.002] Received complete proposal block             module=consensus height=2 hash=CE466AC15102556F98120CEB5B67D1D8F4ACE7E1ED4C71691B37BF5F2F93CC56
node1    | I[2020-02-11|12:59:21.002] enterPrevote(2/0). Current: 2/0/RoundStepPropose module=consensus
node1    | I[2020-02-11|12:59:21.006] enterPrevote: ProposalBlock is valid         module=consensus height=2 round=0
node1    | I[2020-02-11|12:59:21.013] Signed and pushed vote                       module=consensus height=2 round=0 vote="Vote{2:943E47C319AC 2/00/1(Prevote) CE466AC15102 46B9E5530DCE @ 2020-02-11T12:59:21.0132545Z BLSSignature: []}" err=null
node1    | I[2020-02-11|12:59:21.014] Added to prevote                             module=consensus vote="Vote{2:943E47C319AC 2/00/1(Prevote) CE466AC15102 46B9E5530DCE @ 2020-02-11T12:59:21.0132545Z BLSSignature: []}" prevotes="VoteSet{H:2 R:0 T:1 +2/3:<nil>(0.5) BA{4:_xx_} map[]}"
node1    | I[2020-02-11|12:59:21.105] Added to prevote                             module=consensus vote="Vote{3:C427D3A450D9 2/00/1(Prevote) CE466AC15102 921C143194FF @ 2020-02-11T12:59:21.0145391Z BLSSignature: []}" prevotes="VoteSet{H:2 R:0 T:1 +2/3:CE466AC15102556F98120CEB5B67D1D8F4ACE7E1ED4C71691B37BF5F2F93CC56:1:2FB758004ECE(0.75) BA{4:_xxx} map[]}"
node1    | I[2020-02-11|12:59:21.105] Updating ValidBlock because of POL.          module=consensus validRound=-1 POLRound=0
node1    | I[2020-02-11|12:59:21.105] enterPrecommit(2/0). Current: 2/0/RoundStepPrevote module=consensus height=2 round=0
node1    | I[2020-02-11|12:59:21.105] enterPrecommit: +2/3 prevoted proposal block. Locking module=consensus height=2 round=0 hash=CE466AC15102556F98120CEB5B67D1D8F4ACE7E1ED4C71691B37BF5F2F93CC56
node1    | I[2020-02-11|12:59:21.111] Signed and pushed vote                       module=consensus height=2 round=0 vote="Vote{2:943E47C319AC 2/00/2(Precommit) CE466AC15102 D3BC21A5B5F5 @ 2020-02-11T12:59:21.1112359Z BLSSignature: [0 1 6 249 147 27 134 191 233 159 116 223 118 207 196 170 16 42 213 35 143 22 134 205 121 243 147 85 84 162 245 153 84 39 116 123 214 78 191 40 215 146 183 149 242 150 251 220 158 207 121 133 188 165 36 137 118 193 69 101 128 19 123 107 248 207]}" err=null
node1    | I[2020-02-11|12:59:21.119] Added to precommit                           module=consensus vote="Vote{2:943E47C319AC 2/00/2(Precommit) CE466AC15102 D3BC21A5B5F5 @ 2020-02-11T12:59:21.1112359Z BLSSignature: [0 1 6 249 147 27 134 191 233 159 116 223 118 207 196 170 16 42 213 35 143 22 134 205 121 243 147 85 84 162 245 153 84 39 116 123 214 78 191 40 215 146 183 149 242 150 251 220 158 207 121 133 188 165 36 137 118 193 69 101 128 19 123 107 248 207]}" precommits="VoteSet{H:2 R:0 T:2 +2/3:<nil>(0.25) BA{4:__x_} map[]}"
```

##### Scenario No. 2: Cold Start

Starting the testnet with pre-generated BLS keys is not a convenient way to go, but Arcade can not generate new blocks without BLS keys. In case when keys are not provided, an initial round of off-chain DKG is run between the nodes provided as persistent peers in configuration.  

```
cd randapp
rm -rf ./build
make build-docker-rdnode
make build-linux
make localnet-stop
make localnet-start-without-bls-keys
```

Expected output of `docker logs -f $(docker ps -a -q)`:

```
I[2020-02-18|20:02:44.578] OffChainDKG: next verifier is nil, not changing verifier 0=1
E[2020-02-18|20:02:44.579] Can't add peer's address to addrbook         module=p2p err="Cannot add non-routable address 0e15b2983873191b4dc8ebf00d1d4018f4e7f57a@192.168.10.3:26656"
E[2020-02-18|20:02:44.579] Can't add peer's address to addrbook         module=p2p err="Cannot add non-routable address 3f1fe2c94ae877fff6dd3fe4dc8b6c3457b4af64@192.168.10.2:26656"
E[2020-02-18|20:02:44.579] Can't add peer's address to addrbook         module=p2p err="Cannot add non-routable address bb34bb93a741363242632faa6045a73ee9e540fa@192.168.10.4:26656"
I[2020-02-18|20:02:51.601] OffChainDKG: starting round                  round_id=1
<...>
<...>
<...>
I[2020-02-18|20:02:52.236] dkgState: received Commit message            from=6D34AB1347EFC880FB0185D7BD169F2857F7D2DB
I[2020-02-18|20:02:52.236] dkgState: processing commits
D[2020-02-18|20:02:52.246] DKG process commits success
D[2020-02-18|20:02:52.246] complaints messages count is not enough      commits=0 quallen=3
D[2020-02-18|20:02:52.246] DKGDealer Transition not ready               transitioncurrentlength=2
I[2020-02-18|20:02:52.246] dkgState: verifier is ready, killing older rounds
I[2020-02-18|20:02:52.246] handle off-chain share success
I[2020-02-18|20:02:52.246] dkgState: time to update verifier            20=-1
time="2020-02-18T20:02:52Z" level=info msg="New Round validators: &types.ValidatorSet{Validators:[]*types.Validator{(*types.Validator)(0xc000468980), (*types.Validator)(0xc0004689c0), (*types.Validator)(0xc000468a00), (*types.Validator)(0xc000468a40)}, Proposer:(*types.Validator)(0xc000aafb40), totalVotingPower:0}"
I[2020-02-18|20:02:52.582] Executed block                               module=state height=1 validTxs=0 invalidTxs=0
I[2020-02-18|20:02:52.599] Committed state                              module=state height=1 txs=0 
```

##### Scenario No. 3: Successful Off-Chain DKG

DKG should happen every N blocks; so if we start with, say, DKGNumBlocks=10, after 10 blocks we should see a successful Off-Chain DKG round.

 
```
cd randapp
rm -rf ./build
make build-docker-rdnode
make build-linux
make localnet-stop
make localnet-start-with-dkg-in-10-blocks
```

Expected output of `docker logs -f $(docker ps -a -q)`:

```
I[2020-02-18|20:20:20.781] starting ABCI with Tendermint                module=main
ERROR: server config file not found, error: Config File "server" Not Found in "[/rd/node1/rd/config]"
Server Config:
 &{ExampleMetric:0 ChainName:rchain}
load bls from /rd/node1/rd/config/bls_key.json
time="2020-02-18T20:20:26Z" level=info msg="New Round validators: &types.ValidatorSet{Validators:[]*types.Validator{(*types.Validator)(0xc000b0a9c0), (*types.Validator)(0xc000b0aa00), (*types.Validator)(0xc000b0aa40), (*types.Validator)(0xc000b0aa80)}, Proposer:(*types.Validator)(0xc000b0be80), totalVotingPower:0}"
I[2020-02-18|20:20:26.472] Executed block                               module=state height=1 validTxs=0 invalidTxs=0
I[2020-02-18|20:20:26.492] Committed state                              module=state height=1 txs=0 
<...>
<...>
<...>
rnd=7214176971479347127086846303986553651671219432908262786836027876441920614815808726912574261835420662479700247815260736639218325332631015998909403181735761 rndHash=420ECBDBC014F741F23879D7852295088C1B82D7EDABC8FD123A931C835F14B5 appHash=C1801436B1548146731F5F9ADCC57D75D7FD101AC7361F5569282ED1F753C9FB
time="2020-02-18T20:20:42Z" level=info msg="New Round validators: &types.ValidatorSet{Validators:[]*types.Validator{(*types.Validator)(0xc002b256c0), (*types.Validator)(0xc002b25700), (*types.Validator)(0xc002b25740), (*types.Validator)(0xc002b25780)}, Proposer:(*types.Validator)(0xc0028bd600), totalVotingPower:400}"
I[2020-02-18|20:20:42.399] Executed block                               module=state height=4 validTxs=0 invalidTxs=0
I[2020-02-18|20:20:42.432] Committed state                              module=state height=4 txs=0 rnd=6340054489240080883321152661462370346551512084228052128876022924061083414940668566223577594515197313999169519298633242408533441803060231714083105410293790 rndHash=F652D1083549050DD2434DD557E9B48E95E0F1A9AC11FD1427B0F985785D2B34 appHash=6A4CE05064B1C576A90D925BFDE52EAB248408AB45204D1C4846A05B7EECC85D
I[2020-02-18|20:20:42.435] OffChainDKG: starting round                  round_id=1
I[2020-02-18|20:20:42.438] dkgState: sending pub key                    key="bn256.G2:((4957996862c0ccf1f1496971ce9d090939e3669d60a147ff465141b2d2b179f0, 721bbb7bb3ff0e7240d6b3c4bf40efa526ef8a58c3f7e79d298705ed37ffab76), (378a1515f750577d3a7dc27ca8f91d618022f1745ff42b03d2a496a0e86bb189, 25df3e936345af1c21ff8aa83b191c21cdea41327a58551d1168cd522cc9881a))"
I[2020-02-18|20:20:42.438] DKG: msg signed with signature               signature=030488daeeb9a52af27508a1b9493203939bd88e34a21c11e2393669776be2e7b0506006b019afb0558e1f21e3202a4743bafc302894fdf81f81cda249a1e900
D[2020-02-18|20:20:42.438] dkgState: received message with signature:   signature=030488daeeb9a52af27508a1b9493203939bd88e34a21c11e2393669776be2e7b0506006b019afb0558e1f21e3202a4743bafc302894fdf81f81cda249a1e900
I[2020-02-18|20:20:42.439] DKG: message verified
I[2020-02-18|20:20:42.439] dkgState: received PubKey message            from=D444772D46410D90D57601F97E24BA750013CC15 own=D444772D46410D90D57601F97E24BA750013CC15
<...>
<...>
<...>
I[2020-02-18|20:20:42.995] dkgState: received Commit message            from=B0D04D82345739A210104CE7AF4D7926846853D7
I[2020-02-18|20:20:42.995] dkgState: processing commits
D[2020-02-18|20:20:43.004] DKG process commits success
D[2020-02-18|20:20:43.005] complaints messages count is not enough      commits=0 quallen=3
D[2020-02-18|20:20:43.005] DKGDealer Transition not ready               transitioncurrentlength=2
I[2020-02-18|20:20:43.005] dkgState: verifier is ready, killing older rounds
I[2020-02-18|20:20:43.005] handle off-chain share success
time="2020-02-18T20:20:47Z" level=info msg="New Round validators: &types.ValidatorSet{Validators:[]*types.Validator{(*types.Validator)(0xc002e39500), (*types.Validator)(0xc002e39540), (*types.Validator)(0xc002e39580), (*types.Validator)(0xc002e395c0)}, Proposer:(*types.Validator)(0xc002b25680), totalVotingPower:400}"
I[2020-02-18|20:20:47.718] Executed block                               module=state height=5 validTxs=0 invalidTxs=0
I[2020-02-18|20:20:47.741] Committed state                              module=state height=5 txs=0 rnd=3817445511747106503185547658397696293464751164848280796591997319666492698090029915627894234403636219406563185348572009040168425098731301976623311848695884 rndHash=4E973B4BB51C19A0CEB2B5FE89292985BF57F40331522113946343E52DCEA1D3 appHash=7A23EA4A82769A1E5CD654A1BD091CA7DAC91A9DC8B651A8463E4B03869DB081

```

##### Scenario No. 4: Off-Chain fails, Successful On-Chain

If Off-Chain DKG fails for some reason, Arcade switches to  On-Chain DKG. To get an Off-Chain round to fail, we can restart a node during a round:

```
rm -rf ./build
make build-docker-rdnode
make build-linux
make localnet-stop
make localnet-start


# Wait for DKG to start...

docker restart <node_container_id>
```

After several blocks On-Chain DKG should successfully finish.
