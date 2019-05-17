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

## A Note on Production Readiness

DGaming Arcade is not production ready, and Tendermint it's based on is already deployed in public networks but not yet battle-tested enough.

## Minimum requirements

Requirement|Notes
---|---
Go version | Go1.11.4 or higher

## Documentation

Complete documentation for Tendermint can be found on the [website](https://tendermint.com/docs/).

DGaming Arcade documentation TBD.

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

