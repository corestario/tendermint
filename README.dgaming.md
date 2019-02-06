### Overview of DGAMING FOUNDATION BLS updates

Random is implemented as follows. We provide each validator with her own t-of-n BLS keypair share. We also provide each validator with information about other validator's public keys and ID, and this information is structured as a mapping from their normal tendermint `crypto.Address` to respective public keys. The last piece of information that we give to each validator is the master public key that is used to recover aggregate signatures.

Keys must be generated beforehand.

Each validator signs the random data from a previous block (for block height = 1 the value is constant) with her own private key and attaches this signature to her precommit vote. Then she waits for 2/3+1 votes from other validators and tries to recover aggregate signature using the partial signatures received. In case of success, this signature is written to current block header as random data.

### Installation and usage guide

BLS library is included as a git submodule. You have to initialize this submodule before trying to build code.

```
$ git submodule init
$ git submodule update
```

Current implementation requires adding certain libraries to `/usr/local/lib`. Run this from project base directory:

```
$ make install_bls
```

After that you can build and run the node by running:

```
$ make build && ./build/tendermint init && ./build/tendermint node --proxy_app=kvstore
```

Tests should be run like this:

```
make test
```

Tendermint behaves very peculiarly when it comes to printing something inside tests, so I highly recommend running tests with verbose output when debugging:

```
make test_verbose
```

**NOTE:** If you try to build the code manually, without using Makefile directives, you'll have provide correct linker flags; see `$PATH_VAL` in Makefile for more information. Note that this also holds for tests.

### TODOs
 
* I had to remove `CGO_ENABLED=0` from the `make build` directive. Here is a @todo : we have to fix/investigate `make build_c`, `make build_race`, `make install` and `make install_c` directives to make them work as expected.   
* Go can not cross-compile code that uses CGO, so you should use a (virtual) Linux machine for running this code in a cluster. We might want to facilitate this task for MacOS users somehow.
* There's currently no code that solves the task of creating valid genesis files for nodes populated by the `make localnet-start` directive, but it should be implemented. I mean, we should generate keys and automatically update genesis data.
* We should, in general, review and improve the code that works with genesis files. When `--home` directory (the one containing config and data) is not specified, for single node the 1-of-2 keyset is hardcoded inside `cmd/tendermint/commands/init.go`. The problem is that the generated genesis file, which can (by default) be found at `~/.tendermint/config/genesis.json`, is just a file providing information about genesis; the node itself uses genesis data stored in LevelDB, found at `~/.tendermint/data/`, so modifying this data for e.g. cluster nodes is a bit inconvenient.
* Making some tests pass with a real verifier is *very* time-consuming, so I used a MockVerifier. We might want to eliminate any usages of MockVerifier in our tests.
