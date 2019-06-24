# Arcade: Tendermint hack with built-in threshold BLS random beacon for applications

We’re open sourcing our work on Tendermint/CosmosSDK hack with distributed PRNG suitable for applications. 

## Notable repositories
1. https://github.com/dgamingfoundation/tendermint - Tendermint consensus extended with a BLS-based threshold random number generator (both off-chain and on-chain for transparent DKG process). 
2. https://github.com/dgamingfoundation/randapp - a cosmos application that knows how to run DKG rounds. It can be used as a template for any application that needs random numbers generation (much like the [Baseapp](https://cosmos.network/docs/concepts/baseapp.html)).
3. https://github.com/dgamingfoundation/dkglib - a kyber-based pure Go library that implements DKG-related procedures.
 
## What do we want from our distributed random?

We want our in-built PRNG to be suitable for applications like games and gambling: that means fast, unbiasable (many random beacons used for consensus are slightly biasable; what's fine for cryptographic sortition is not fine for a game of blackjack). Preferably it should be available at every block. 



1. Unbiasable
2. 2 seconds round (preferably 1 sec)
3. Publicly verifiable
4. Small receipt (under 10kb)
5. Decentralizible 
6. Safety and liveness similar to vanilla Tendermint
7. Sybil resistant
8. Censorship resistant

The most suitable random beacon to satisfy constraints is BLS Threshold siganture-based beacon, proposed by DFinity team. There will be a lengthy state of knowledge post on that, but it’s basically the only one that’s good enough and fast enough for our purpose. 

We expanded a precommit step in the Tendermint consensus:  

![Arcade consensus](https://github.com/dgamingfoundation/tendermint/blob/dcr-random/docs/imgs/arcade_consensus.png?raw=true)

That scheme, among other things, means that block proposer for the round selects txs in the block before the random number for the block is known. 

BLS threshold signatures need a key generation step. We implemented an off-chain distributed key generation based on "Secure distributed key generation for discrete-log based cryptosystems", 2007 by Gennaro et al. Now we're working towards making a permissionless on-chain one - much more complicated affair given it needs to have PoS mechanics, slashing, validator set change and so on. 

Design of threshold BLS friendly PoS mechanics was the hardest part by far. 

Given:
1. Blockchain consensus and BLS random beacon is done by the same set of validators
2. Threshold BLS is essentially one node-one vote as opposed to fractional voting power of vanilla CosmosSDK staking
3. Validators are selected by stake (i.e. top 100 validators by stake)
4. Changing BLS random beacon participant set involves DKG that has no guarantees to be successful
5. A single non-cooperating party will make DKG for that particular set of validators fail.
How do we design a PoS given this fragile DKG process?

Tools in our disposal that we can combine to design a somewhat robust protocol:
1. Disqualify/slash uncooperative validators. If a validator refuses to follow DKG protocol they are replaced with a candidate with most stake available.
2. DKG trigger thresholds. Do not trigger validator set change and corresponding DKG unless there is a noticeable change is stake distribution  - let's say until 5+% of voting power changed.
3. On-chain DKG. We can put DKG on-chain (partly or fully) so that we can punish non-cooperating parties in some ways
4. Validator set change limit. We can put a limit to maximum validator set change between DKG instances
5. Delayed validator set change. We can delay validator set change until DKG for the new set is over. 
6. We can change validators in epochs, with epoch not ending until DKG is over.
7. Randomless mode. We can make blockchain continue without BLS beacon temporarily until DKG is over.
8. Stop the world. We can halt the chain progress until the off-chain DKG is over.

We’re currently working on a protocol described below:
Every node with a stake of X+ coins is eligible to be a validator. 1 node = 1 voice, there is no difference for staking X or 2X coins apart from how much slashing you can take before losing voting rights. There is an upper cap for validator limit (about a hundred). 
1. Chain starts with off-chain DKG from genesis stake distribution.   
2. DKG is triggered either by a big enough shift in stakes (i.e. >5%) or by small shift + long enough period of time (i.e. 2 days).
3. Optimistic off-chain DKG starts and doesn’t clog the blockchain space unless there are any malicious validators.
4. If off-chain DKG fails, we fall back on fully on-chain Gennaro et al. DKG with small slashing of byzantine actors. If DKG fails, slash offending parties and retry.
5. Delay validator set change until DKG is over.
