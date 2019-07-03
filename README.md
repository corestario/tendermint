# DGaming Arcade

[Byzantine-Fault Tolerant](https://en.wikipedia.org/wiki/Byzantine_fault_tolerance)
[State Machines](https://en.wikipedia.org/wiki/State_machine_replication).
Or [Blockchain](https://en.wikipedia.org/wiki/Blockchain_(database)), for short.

[![version](https://img.shields.io/github/tag/tendermint/tendermint.svg)](https://github.com/tendermint/tendermint/releases/latest)
[![API Reference](
https://camo.githubusercontent.com/915b7be44ada53c290eb157634330494ebe3e30a/68747470733a2f2f676f646f632e6f72672f6769746875622e636f6d2f676f6c616e672f6764646f3f7374617475732e737667
)](https://godoc.org/github.com/tendermint/tendermint)
[![Go version](https://img.shields.io/badge/go-1.10.4-blue.svg)](https://github.com/moovweb/gvm)
[![riot.im](https://img.shields.io/badge/riot.im-JOIN%20CHAT-green.svg)](https://riot.im/app/#/room/#tendermint:matrix.org)
[![license](https://img.shields.io/github/license/tendermint/tendermint.svg)](https://github.com/tendermint/tendermint/blob/master/LICENSE)
[![](https://tokei.rs/b1/github/tendermint/tendermint?category=lines)](https://github.com/tendermint/tendermint)


Branch    | Tests | Coverage
----------|-------|----------
dcr-random    | [![CircleCI](https://circleci.com/gh/dgamingfoundation/tendermint/tree/dcr-random.svg?style=svg&circle-token=3f83767e96915a51aee5e7866e3bd6cb9130e9db)](https://circleci.com/gh/dgamingfoundation/tendermint/tree/dcr-random) | 

DGaming Arcade is a Tendermint-based Byzantine Fault Tolerant (BFT) middleware that takes a state transition machine - written in any programming language -
and securely replicates it on many machines. It's got an embedded BLS-based random beacon and built-in off-chain and on-chain DKGs.

For Tendermint protocol details, see [the specification](/docs/spec).

For detailed analysis of the Tendermint consensus protocol, including safety and liveness proofs,
see our recent paper, "[The latest gossip on BFT consensus](https://arxiv.org/abs/1807.04938)".

For Arcade details, see [Arcade specification](/docs/arcade/arcade.md)

## A Note on Production Readiness

DGaming Arcade is not production ready, and Tendermint it's based on is already deployed in public networks but not yet battle-tested enough.

## Minimum requirements

Requirement|Notes
---|---
Go version | Go1.11.4 or higher

## Documentation

Complete documentation for Tendermint can be found on the [website](https://tendermint.com/docs/).

## How Arcade is different from vanilla Tendermint

This part of document describes an implementation of BLS-based random beacon that was added to Tendermint consensus by dgaming-labs team.

### Overview

The goal of our project is to provide a framework for supplying cosmos applications with non-centralized source of entropy.  We want our in-built PRNG to be suitable for applications like games and gambling: that means fast, unbiasable, and easy to use. More specifically, our design constraints were:

* Unbiasable
* 2 seconds round (preferably 1 sec)
* Publicly verifiable
* Small receipt (under 10kb) and fast verification
* Not relying on a central party or an oracle to function
* Synchronous model of requesting for random number

Additionally, we wanted to keep vanilla Tendermint's safety, liveness, sybil and censorship resitance under the same security model. 

There are a lot of solutions for random numbers in blockchain that do not conform to this constraints. Some random beacons (most notable is RanDAO) are slightly biasable, which is fine for many applications. Others are not fast enough, not live enough, have receipts too big or require asynchrounos requests (i.e., in block 1000 applications requests a random number and in block 1010 it receives it).   

The result of our work is a Tendermint-based blockchain that provides each block with a random number that can be safely used during block processing. In quorum setting it is as live and as safe as vanilla Tendermint (the difference is basically slightly increased size of consensus-related messages and block headers). Random data (an array of bytes) is added to each block's header and is accessible from application code.

The way this data is obtained is as follows. We generate a t-of-n BLS keyring (using [dedis/kyber](https://github.com/dedis/kyber)) with a public key that is known to all validators and private shares known only to their holders. We add a (non-random) seed value (just any string) to genesis block; then for each new block a validator signs the random value from the previous block with their private key and shares that signature with other validators. When there's enough shares for the t-of-n threshold signature to be recovered, the resulting signature becomes the next random value and is added to block header.

Sharing signatures is implemented by extending Tendermint's `Vote` type with a `.Data []byte` field. Signatures are shared during the Precommit phase, and BLS threshold equals `2/3+1` (so that we can recover the signature as soon as we have a polka):

![Tendermint+BLS](https://github.com/dgamingfoundation/tendermint/raw/dcr-random/docs/imgs/arcade_consensus.png?raw=true)

That scheme, among other things, means that block proposer for the round selects txs in the block before the random number for the block is known.

*important source code files to check out about BLS random beacon*

### DKG

BLS random beacon requires a distributed key generation process. It is initiated before the first block is minted, and every  N blocks to generate a new BLS keyring (that's to reduce a potential impact of a stolen key). We implemented a distributed key generation based on "Secure distributed key generation for discrete-log based cryptosystems", 2007 by Gennaro et al.; the [dedis/kyber](https://github.com/dedis/kyber) implementation of DKG is used. Messaging is done in two different ways: *off-chain* and *on-chain*.


##### Off-chain DKG

Off-chain DKG uses Tendermint's messaging engine to deliver DKG-related data to validator peers *without* writing anything to blocks. When it's time to generate a new BLS keyring, we first try to run an off-chain DKG round because it's cheap and fast; if everything goes O.K., the new keyring is used for producing new blocks. Off-chain DKG runs in parallel with consensus. The problem with off-chain DKG is that you can cannot (due to lack of evidence) slash validators that do not participate in DKG; if off-chain DKG fails, we switch to on-chain DKG.

Notable source code files to check out about DKG:

1. https://github.com/dgamingfoundation/tendermint/blob/dcr-random/types/random.go
2. https://github.com/dgamingfoundation/tendermint/blob/dcr-random/consensus/dkg.go
3. https://github.com/dgamingfoundation/tendermint/blob/dcr-random/consensus/dkg_dealer.go

Note that most DKG-related code will be moved to [dkglib](https://github.com/dgamingfoundation/dkglib)) (see *Randapp* section below) when On-Chain DKG is finally implemented.


We implemented DKG for a quorum (permissioned network of equipowerful validators), and are working on a PoS implementation. We’re currently working on a protocol described below:
Every node with a stake of X+ coins is eligible to be a validator. 1 node = 1 voice, there is no difference for staking X or 2X coins apart from how much slashing you can take before losing voting rights. There is an upper cap for validator limit (about a hundred). 
1. Chain starts with off-chain DKG from genesis stake distribution.   
2. DKG is triggered either by a big enough shift in stakes (i.e., >5%) or by small shift + long enough period of time (i.e., 2 days).
3. Optimistic off-chain DKG starts and doesn’t clog the blockchain space unless there are any malicious validators.
4. If off-chain DKG fails, we fall back on fully on-chain Gennaro et al. DKG with small slashing of byzantine actors. If DKG fails, slash offending parties and retry.
5. Delay validator set change until DKG is over.

##### On-chain DKG (WIP)

On-chain DKG works the same way as the off-chain version but writes its messages to blocks, which allows us to slash a validator that refuses to participate in a DKG round.


##### Randapp

[Randapp](https://github.com/dgamingfoundation/randapp) (currently work in progress) is a Cosmos application that can handle DKG-related messages (that are supposed to be sent by the [dkglib](https://github.com/dgamingfoundation/dkglib)). This application is currently implemented as a standalone one, but in the future its functionality will be available as a pluggable module (same way as `/x/auth` or `/x/bank` provides its methods and routes).


### Install

See the [install instructions](/docs/introduction/install.md)

### Quick Start

- [Single node](/docs/introduction/quick-start.md)
- [Local cluster using docker-compose](/docs/networks/docker-compose.md)
- [Remote cluster using terraform and ansible](/docs/networks/terraform-and-ansible.md)
- [Join the Cosmos testnet](https://cosmos.network/testnet)

## Contributing

Please abide by the [Code of Conduct](CODE_OF_CONDUCT.md) in all interactions,
and the [contributing guidelines](CONTRIBUTING.md) when submitting code.

Join the larger community on the [forum](https://forum.cosmos.network/) and the [chat](https://riot.im/app/#/room/#tendermint:matrix.org).

To learn more about the structure of the software, watch the [Developer
Sessions](https://www.youtube.com/playlist?list=PLdQIb0qr3pnBbG5ZG-0gr3zM86_s8Rpqv)
and read some [Architectural
Decision Records](https://github.com/tendermint/tendermint/tree/master/docs/architecture).

Learn more by reading the code and comparing it to the
[specification](https://github.com/tendermint/tendermint/tree/develop/docs/spec).

## Versioning

### Semantic Versioning

Tendermint uses [Semantic Versioning](http://semver.org/) to determine when and how the version changes.
According to SemVer, anything in the public API can change at any time before version 1.0.0

To provide some stability to Tendermint users in these 0.X.X days, the MINOR version is used
to signal breaking changes across a subset of the total public API. This subset includes all
interfaces exposed to other processes (cli, rpc, p2p, etc.), but does not
include the in-process Go APIs.

That said, breaking changes in the following packages will be documented in the
CHANGELOG even if they don't lead to MINOR version bumps:

- types
- rpc/client
- config
- node
- libs
  - bech32
  - common
  - db
  - errors
  - log

Exported objects in these packages that are not covered by the versioning scheme
are explicitly marked by `// UNSTABLE` in their go doc comment and may change at any
time without notice. Functions, types, and values in any other package may also change at any time.

### Upgrades

In an effort to avoid accumulating technical debt prior to 1.0.0,
we do not guarantee that breaking changes (ie. bumps in the MINOR version)
will work with existing tendermint blockchains. In these cases you will
have to start a new blockchain, or write something custom to get the old
data into the new chain.

However, any bump in the PATCH version should be compatible with existing histories
(if not please open an [issue](https://github.com/tendermint/tendermint/issues)).

For more information on upgrading, see [UPGRADING.md](./UPGRADING.md)

## Resources

### Tendermint Core

For details about the blockchain data structures and the p2p protocols, see the
[Tendermint specification](/docs/spec).

For details on using the software, see the [documentation](/docs/) which is also
hosted at: https://tendermint.com/docs/

### Tools

Benchmarking and monitoring is provided by `tm-bench` and `tm-monitor`, respectively.
Their code is found [here](/tools) and these binaries need to be built seperately.
Additional documentation is found [here](/docs/tools).

### Sub-projects

* [Amino](http://github.com/tendermint/go-amino), reflection-based proto3, with
  interfaces
* [IAVL](http://github.com/tendermint/iavl), Merkleized IAVL+ Tree implementation

### Applications

* [Cosmos SDK](http://github.com/cosmos/cosmos-sdk); a cryptocurrency application framework
* [Ethermint](http://github.com/cosmos/ethermint); Ethereum on Tendermint
* [Many more](https://tendermint.com/ecosystem)

### Research

* [The latest gossip on BFT consensus](https://arxiv.org/abs/1807.04938)
* [Master's Thesis on Tendermint](https://atrium.lib.uoguelph.ca/xmlui/handle/10214/9769)
* [Original Whitepaper](https://tendermint.com/static/docs/tendermint.pdf)
* [Blog](https://blog.cosmos.network/tendermint/home)

