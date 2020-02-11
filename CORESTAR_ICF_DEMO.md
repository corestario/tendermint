### Demo scenarios for Corestar-ICF Acrade Grant

##### Recap

In 2019 Corestar received an ICF grant for an implementation of random data generation inside Tendermint consensus. This document describes the scenarios that aim to demonstrate current progress and state of development.

##### Scenario No. 1: Run with pre-generated BLS keys

Arcade's random data generation is based on threshold BLS signature recovery, where the recovered signature is put into block header. For a network of nodes to run, we can put pre-generated BLS keys into each node's data directory and run the testnet. This is currently done automatically, and the procedure is not different from vanilla local testnet initialization:

```
rm -rf ./build
make build-docker-localnode
make build-linux
make localnet-stop
make localnet-start
```    

Expected output of `docker logs -f a97c87d40479`, with an appropriate container ID:

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
rm -rf ./build
make build-docker-localnode
make build-linux
make localnet-stop
make localnet-start --cold
```

Expected output of `docker logs -f a97c87d40479`, with an appropriate container ID:

```
node1    | I[2020-02-11|14:07:07.098] OffChainDKG: starting round                  round_id=1
node1    | I[2020-02-11|14:07:07.100] Timed out                                    module=consensus dur=-40.2376ms height=1 round=0 step=RoundStepNewHeight
node1    | I[2020-02-11|14:07:07.101] dkgState: sending pub key                    key="bn256.G2:((21cc31ef58589a9d15ff0589f313c9d3d4b1567fa27f1bdc85e590fdaf052827, 2a5a4cf94262444c272d60f17244780a1560b27ebb5ebfe47291444bbfe7bd72), (71a684412a86a41c52c804c5a0d5420724a52c17aa0e8607e20036d9f19cb3dd, 222b3c898f26bfb32c54a00e2501eb764573cb88bdb90e8ded5ab12aeb8ee050))"
node1    | I[2020-02-11|14:07:07.102] DKG: msg signed with signature               signature=410ad01ed0102ae7e317d270e69454f765f344c1cc1b9f3df554221c0f3deb73e2be8f00e2145ea48732d758bb20f2aedcbc045790333da09ea4147101c4f705
node1    | D[2020-02-11|14:07:07.102] dkgState: received message with signature:   signature=410ad01ed0102ae7e317d270e69454f765f344c1cc1b9f3df554221c0f3deb73e2be8f00e2145ea48732d758bb20f2aedcbc045790333da09ea4147101c4f705
node1    | I[2020-02-11|14:07:07.103] DKG: message verified
node1    | I[2020-02-11|14:07:07.103] dkgState: received PubKey message            from=6FA3FB86339854CBC5AF659159860590FBA29E99
node1    | I[2020-02-11|14:07:07.103] dkgState: received PubKey message            from=6FA3FB86339854CBC5AF659159860590FBA29E99
node1    | I[2020-02-11|14:07:07.104] handled off-chain dkg message, verifier is not ready yet module=consensus
node1    | I[2020-02-11|14:07:07.196] Starting Peer                                module=p2p peer=7f290ff86224c58f46d6cdff44469c9ded5a983c@192.167.10.5:38524 impl="Peer{MConn{192.167.10.5:38524} 7f290ff86224c58f46d6cdff44469c9ded5a983c in}"
node1    | I[2020-02-11|14:07:07.196] Starting MConnection                         module=p2p peer=7f290ff86224c58f46d6cdff44469c9ded5a983c@192.167.10.5:38524 impl=MConn{192.167.10.5:38524}
node1    | I[2020-02-11|14:07:07.196] Added peer                                   module=p2p peer="Peer{MConn{192.167.10.5:38524} 7f290ff86224c58f46d6cdff44469c9ded5a983c in}"
node1    | D[2020-02-11|14:07:07.590] dkgState: received message with signature:   signature=908681df13bd22e845005abf9c1d4efbed1851f505f871cb7d1676769335d16b2f28c7ecdd88f822a14fdda28f6283cd424904710659fa53a1957fc0bf0ef503
node1    | I[2020-02-11|14:07:07.590] DKG: message verified
node1    | I[2020-02-11|14:07:07.590] dkgState: received PubKey message            from=7A74BD6A9702850BFEB01B3ABF11C09E88E9AC2E
node1    | I[2020-02-11|14:07:07.590] dkgState: received PubKey message            from=7A74BD6A9702850BFEB01B3ABF11C09E88E9AC2E
```

##### Scenario No. 3: Successful Off-Chain DKG

DKG should happen every N blocks; so if we start with, say, DKGNumBlocks=50, after 50 blocks we should see a successful Off-Chain DKG round.

 
```
rm -rf ./build
make build-docker-localnode
make build-linux
make localnet-stop
make localnet-start --dkg_num_blocks=50
```

##### Scenario No. 4: Off-Chain fails, Successful On-Chain

If Off-Chain DKG fails for some reason, Arcade switches to  On-Chain DKG. To get an Off-Chain round to fail, we can restart a node during a round:

```
rm -rf ./build
make build-docker-localnode
make build-linux
make localnet-stop
make localnet-start --dkg_num_blocks=50

# Wait for DKG to start...

docker restart <node_container_id>
```

After several blocks On-Chain DKG should successfully finish.
