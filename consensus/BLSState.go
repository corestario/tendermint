package consensus

import (
	"fmt"
	"runtime/debug"

	"github.com/dgamingfoundation/tendermint/state"

	dkgtypes "github.com/dgamingfoundation/dkglib/lib/types"
	cfg "github.com/tendermint/tendermint/config"
	cstypes "github.com/tendermint/tendermint/consensus/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	tmevents "github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/fail"
	"github.com/tendermint/tendermint/p2p"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

type BLSConsensusState struct {
	ConsensusState
	dkg dkgtypes.DKG
}

type BLSStateOption func(*BLSConsensusState)

var _ StateInterface = &BLSConsensusState{}

func NewBLSConsensusState(
	config *cfg.ConsensusConfig,
	state sm.State,
	blockExec *sm.BlockExecutor,
	blockStore sm.BlockStore,
	txNotifier txNotifier,
	evpool evidencePool,
	options ...BLSStateOption,
) *BLSConsensusState {
	blsCS := &BLSConsensusState{}
	blsCS.ConsensusState = ConsensusState{
		config:           config,
		blockExec:        blockExec,
		blockStore:       blockStore,
		txNotifier:       txNotifier,
		peerMsgQueue:     make(chan msgInfo, msgQueueSize),
		internalMsgQueue: make(chan msgInfo, msgQueueSize),
		timeoutTicker:    NewTimeoutTicker(),
		statsMsgQueue:    make(chan msgInfo, msgQueueSize),
		done:             make(chan struct{}),
		doWALCatchup:     true,
		wal:              nilWAL{},
		evpool:           evpool,
		evsw:             tmevents.NewEventSwitch(),
		metrics:          NopMetrics(),
	}
	blsCS.BaseService = *cmn.NewBaseService(nil, "BLSConsensusState", blsCS)
	// set function defaults (may be overwritten before calling Start)
	blsCS.decideProposal = blsCS.defaultDecideProposal
	blsCS.doPrevote = blsCS.defaultDoPrevote
	blsCS.setProposal = blsCS.defaultSetProposal

	for _, option := range options {
		option(blsCS)
	}

	blsCS.updateToState(state)

	// Don't call scheduleRound0 yet.
	// We do that upon Start().
	blsCS.reconstructLastCommit(state)

	return blsCS
}

func (cs *BLSConsensusState) updateHeight(height int64) {
	cs.metrics.Height.Set(float64(height))
	cs.Height = height

	// TODO (oopcode): as we make ConsensusState an interface, we will feed different
	// states (and the standard one will be without this dkg field). Currently update height
	// is called _before_ the actual start of consensus, which leads to panic (because
	// node.Options have not been applied yet).
	if cs.dkg != nil {
		cs.dkg.CheckDKGTime(cs.Height, cs.Validators)
	}
}

func (cs *BLSConsensusState) SetVerifier(verifier dkgtypes.Verifier) {
	cs.dkg.SetVerifier(verifier)
}

func (cs *BLSConsensusState) receiveRoutine(maxSteps int) {
	onExit := func(cs *BLSConsensusState) {
		// NOTE: the internalMsgQueue may have signed messages from our
		// priv_val that haven't hit the WAL, but its ok because
		// priv_val tracks LastSig

		// close wal now that we're done writing to it
		cs.wal.Stop()
		cs.wal.Wait()

		close(cs.done)
	}

	defer func() {
		if r := recover(); r != nil {
			cs.Logger.Error("CONSENSUS FAILURE!!!", "err", r, "stack", string(debug.Stack()))
			// stop gracefully
			//
			// NOTE: We most probably shouldn't be running any further when there is
			// some unexpected panic. Some unknown error happened, and so we don't
			// know if that will result in the validator signing an invalid thing. It
			// might be worthwhile to explore a mechanism for manual resuming via
			// some console or secure RPC system, but for now, halting the chain upon
			// unexpected consensus bugs sounds like the better option.
			onExit(cs)
		}
	}()

	for {
		if maxSteps > 0 {
			if cs.nSteps >= maxSteps {
				cs.Logger.Info("reached max steps. exiting receive routine")
				cs.nSteps = 0
				return
			}
		}
		rs := cs.RoundState
		var mi msgInfo

		select {
		case msg := <-cs.dkg.MsgQueue():
			cs.dkg.HandleOffChainShare(msg, cs.Height, cs.Validators, cs.privValidator.GetPubKey())
		case <-cs.txNotifier.TxsAvailable():
			cs.handleTxsAvailable()
		case mi = <-cs.peerMsgQueue:
			cs.wal.Write(mi)
			// handles proposals, block parts, votes
			// may generate internal events (votes, complete proposals, 2/3 majorities)
			cs.handleMsg(mi)
		case mi = <-cs.internalMsgQueue:
			cs.wal.WriteSync(mi) // NOTE: fsync

			if _, ok := mi.Msg.(*VoteMessage); ok {
				// we actually want to simulate failing during
				// the previous WriteSync, but this isn't easy to do.
				// Equivalent would be to fail here and manually remove
				// some bytes from the end of the wal.
				fail.Fail() // XXX
			}

			// handles proposals, block parts, votes
			cs.handleMsg(mi)
		case ti := <-cs.timeoutTicker.Chan(): // tockChan:
			cs.wal.Write(ti)
			// if the timeout is relevant to the rs
			// go to the next step
			cs.handleTimeout(ti, rs)
		case <-cs.Quit():
			onExit(cs)
			return
		}
	}
}

// Enter: +2/3 precommits for block
func (cs *BLSConsensusState) enterCommit(height int64, commitRound int) {
	logger := cs.Logger.With("height", height, "commitRound", commitRound)

	if cs.Height != height || cstypes.RoundStepCommit <= cs.Step {
		logger.Debug(fmt.Sprintf("enterCommit(%v/%v): Invalid args. Current step: %v/%v/%v", height, commitRound, cs.Height, cs.Round, cs.Step))
		return
	}
	logger.Info(fmt.Sprintf("enterCommit(%v/%v). Current: %v/%v/%v", height, commitRound, cs.Height, cs.Round, cs.Step))

	defer func() {
		if err := recover(); err != nil {
			logger.Info("PANIC HANDLED", "PANIC HANDLED", err)
		}
		// Done enterCommit:
		// keep cs.Round the same, commitRound points to the right Precommits set.
		cs.updateRoundStep(cs.Round, cstypes.RoundStepCommit)
		cs.CommitRound = commitRound
		cs.CommitTime = tmtime.Now()
		cs.newStep()

		// Maybe finalize immediately.
		cs.tryFinalizeCommit(height)
	}()

	precommits := cs.Votes.Precommits(commitRound)
	blockID, ok := precommits.TwoThirdsMajority()
	if !ok {
		panic("RunActionCommit() expects +2/3 precommits")
	}

	// The Locked* fields no longer matter.
	// Move them over to ProposalBlock if they match the commit hash,
	// otherwise they'll be cleared in updateToState.
	if cs.LockedBlock.HashesTo(blockID.Hash) {
		logger.Info("Commit is for locked block. Set ProposalBlock=LockedBlock", "blockHash", blockID.Hash)
		cs.ProposalBlock = cs.LockedBlock
		cs.ProposalBlockParts = cs.LockedBlockParts
	}

	randomData, err := cs.dkg.Verifier().Recover(cs.getPreviousBlock().RandomData, precommits.GetVotes())
	if err != nil {
		panic(fmt.Sprintf("Failed to recover random data from votes: %v", err))
	}
	cs.Logger.Info("Generated random data", "rand_data", randomData)
	// TODO @oopcode: check if this is a possible situation.
	if cs.ProposalBlock != nil {
		cs.ProposalBlock.Header.SetRandomData(randomData)
	}

	// If we don't have the block being committed, set up to get it.
	if !cs.ProposalBlock.HashesTo(blockID.Hash) {
		if !cs.ProposalBlockParts.HasHeader(blockID.PartsHeader) {
			logger.Info("Commit is for a block we don't know about. Set ProposalBlock=nil", "proposal", cs.ProposalBlock.Hash(), "commit", blockID.Hash)
			// We're getting the wrong block.
			// Set up ProposalBlockParts and keep waiting.
			cs.ProposalBlock = nil
			cs.ProposalBlockParts = types.NewPartSetFromHeader(blockID.PartsHeader)
			cs.eventBus.PublishEventValidBlock(cs.RoundStateEvent())
			cs.evsw.FireEvent(types.EventValidBlock, &cs.RoundState)
		}
		// else {
		// We just need to keep waiting.
		// }
	}
}

// Increment height and goto cstypes.RoundStepNewHeight
func (cs *BLSConsensusState) finalizeCommit(height int64) {
	if cs.Height != height || cs.Step != cstypes.RoundStepCommit {
		cs.Logger.Debug(fmt.Sprintf("finalizeCommit(%v): Invalid args. Current step: %v/%v/%v", height, cs.Height, cs.Round, cs.Step))
		return
	}

	blockID, ok := cs.Votes.Precommits(cs.CommitRound).TwoThirdsMajority()
	block, blockParts := cs.ProposalBlock, cs.ProposalBlockParts

	if !ok {
		panic(fmt.Sprintf("Cannot finalizeCommit, commit does not have two thirds majority"))
	}
	if !blockParts.HasHeader(blockID.PartsHeader) {
		panic(fmt.Sprintf("Expected ProposalBlockParts header to be commit header"))
	}
	if !block.HashesTo(blockID.Hash) {
		panic(fmt.Sprintf("Cannot finalizeCommit, ProposalBlock does not hash to commit hash"))
	}
	if err := cs.blockExec.ValidateBlock(cs.state, block); err != nil {
		panic(fmt.Sprintf("+2/3 committed an invalid block: %v", err))
	}

	prevBlock := cs.getPreviousBlock()
	if err := cs.dkg.Verifier().VerifyRandomData(prevBlock.Header.RandomData, block.Header.RandomData); err != nil {
		panic(fmt.Sprintf("Cannot finalizeCommit, ProposalBlock has invalid random value: %v", err))
	}
	cs.Logger.Info(fmt.Sprintf("Finalizing commit of block with %d txs", block.NumTxs),
		"height", block.Height, "hash", block.Hash(), "root", block.AppHash)
	cs.Logger.Info(fmt.Sprintf("%v", block))

	fail.Fail() // XXX

	// Save to blockStore.
	if cs.blockStore.Height() < block.Height {
		// NOTE: the seenCommit is local justification to commit this block,
		// but may differ from the LastCommit included in the next block
		precommits := cs.Votes.Precommits(cs.CommitRound)
		seenCommit := precommits.MakeCommit()
		cs.blockStore.SaveBlock(block, blockParts, seenCommit)
	} else {
		// Happens during replay if we already saved the block but didn't commit
		cs.Logger.Info("Calling finalizeCommit on already stored block", "height", block.Height)
	}

	fail.Fail() // XXX

	// Write EndHeightMessage{} for this height, implying that the blockstore
	// has saved the block.
	//
	// If we crash before writing this EndHeightMessage{}, we will recover by
	// running ApplyBlock during the ABCI handshake when we restart.  If we
	// didn't save the block to the blockstore before writing
	// EndHeightMessage{}, we'd have to change WAL replay -- currently it
	// complains about replaying for heights where an #ENDHEIGHT entry already
	// exists.
	//
	// Either way, the ConsensusState should not be resumed until we
	// successfully call ApplyBlock (ie. later here, or in Handshake after
	// restart).
	cs.wal.WriteSync(EndHeightMessage{height}) // NOTE: fsync

	fail.Fail() // XXX

	// Create a copy of the state for staging and an event cache for txs.
	stateCopy := cs.state.Copy()

	dkgLosers := cs.dkg.GetLosers()
	for _, dkgLoser := range dkgLosers {
		// TODO: Currently we lack a lot of relevant information about the failed node,
		// TODO: including the height at which we had a misbehavior. We should address
		// TODO: this as soon as possible.
		if err := cs.evpool.AddEvidence(&state.DKGEvidenceCorruptData{
			DKGEvidence: state.DKGEvidence{
				Loser:           dkgLoser,
				ValidatorPubKey: dkgLoser.Validator.PubKey,
			},
		}); err != nil {
			panic(fmt.Sprintf("failed to add dkg evidence for validator %s: %v", dkgLoser.Validator.String(), err))
		}
	}

	// Execute and commit the block, update and save the state, and update the mempool.
	// NOTE The block.AppHash wont reflect these txs until the next block.
	var err error
	stateCopy, err = cs.blockExec.ApplyBlock(stateCopy, types.BlockID{Hash: block.Hash(), PartsHeader: blockParts.Header()}, block)
	if err != nil {
		cs.Logger.Error("Error on ApplyBlock. Did the application crash? Please restart tendermint", "err", err)
		err := cmn.Kill()
		if err != nil {
			cs.Logger.Error("Failed to kill this process - please do so manually", "err", err)
		}
		return
	}

	fail.Fail() // XXX

	// must be called before we update state
	cs.recordMetrics(height, block)

	// NewHeightStep!
	cs.updateToState(stateCopy)

	fail.Fail() // XXX

	// cs.StartTime is already set.
	// Schedule Round0 to start soon.
	cs.scheduleRound0(&cs.RoundState)

	// By here,
	// * cs.Height has been increment to height+1
	// * cs.Step is now cstypes.RoundStepNewHeight
	// * cs.StartTime is set to when we will start round0.
}

func (cs *BLSConsensusState) getPreviousBlock() *types.Block {
	var prevBlock *types.Block
	if cs.Height == 1 {
		prevBlock = &types.Block{Header: types.Header{RandomData: []byte(types.InitialRandomData)}}
	} else {
		prevBlock = cs.blockStore.LoadBlock(cs.Height - 1)
	}

	return prevBlock
}

func (cs *BLSConsensusState) addVote(vote *types.Vote, peerID p2p.ID) (added bool, err error) {
	cs.Logger.Debug("addVote", "voteHeight", vote.Height, "voteType", vote.Type, "valIndex", vote.ValidatorIndex, "csHeight", cs.Height)

	// A precommit for the previous height?
	// These come in while we wait timeoutCommit
	if vote.Height+1 == cs.Height {
		if !(cs.Step == cstypes.RoundStepNewHeight && vote.Type == types.PrecommitType) {
			// TODO: give the reason ..
			// fmt.Errorf("tryAddVote: Wrong height, not a LastCommit straggler commit.")
			return added, ErrVoteHeightMismatch
		}
		added, err = cs.LastCommit.AddVote(vote)
		if !added {
			return added, err
		}

		cs.Logger.Info(fmt.Sprintf("Added to lastPrecommits: %v", cs.LastCommit.StringShort()))
		cs.eventBus.PublishEventVote(types.EventDataVote{Vote: vote})
		cs.evsw.FireEvent(types.EventVote, vote)

		// if we can skip timeoutCommit and have all the votes now,
		if cs.config.SkipTimeoutCommit && cs.LastCommit.HasAll() {
			// go straight to new round (skip timeout commit)
			// cs.scheduleTimeout(time.Duration(0), cs.Height, 0, cstypes.RoundStepNewHeight)
			cs.enterNewRound(cs.Height, 0)
		}

		return
	}

	// Height mismatch is ignored.
	// Not necessarily a bad peer, but not favourable behaviour.
	if vote.Height != cs.Height {
		err = ErrVoteHeightMismatch
		cs.Logger.Info("Vote ignored and not added", "voteHeight", vote.Height, "csHeight", cs.Height, "peerID", peerID)
		return
	}

	if vote.Type == types.PrecommitType {
		var (
			prevBlockData = cs.getPreviousBlock().RandomData
			validatorAddr = vote.ValidatorAddress.String()
		)
		if err := cs.dkg.Verifier().VerifyRandomShare(validatorAddr, prevBlockData, vote.BLSSignature); err != nil {
			return false, fmt.Errorf("random share authenticy check failed: %v, validator %v, prevBlockData %v, vote.BLSSignature %v",
				err, validatorAddr, prevBlockData, vote.BLSSignature)
		}
	}

	height := cs.Height
	added, err = cs.Votes.AddVote(vote, peerID)
	if !added {
		// Either duplicate, or error upon cs.Votes.AddByIndex()
		return
	}

	cs.eventBus.PublishEventVote(types.EventDataVote{Vote: vote})
	cs.evsw.FireEvent(types.EventVote, vote)

	switch vote.Type {
	case types.PrevoteType:
		prevotes := cs.Votes.Prevotes(vote.Round)
		cs.Logger.Info("Added to prevote", "vote", vote, "prevotes", prevotes.StringShort())

		// If +2/3 prevotes for a block or nil for *any* round:
		if blockID, ok := prevotes.TwoThirdsMajority(); ok {

			// There was a polka!
			// If we're locked but this is a recent polka, unlock.
			// If it matches our ProposalBlock, update the ValidBlock

			// Unlock if `cs.LockedRound < vote.Round <= cs.Round`
			// NOTE: If vote.Round > cs.Round, we'll deal with it when we get to vote.Round
			if (cs.LockedBlock != nil) &&
				(cs.LockedRound < vote.Round) &&
				(vote.Round <= cs.Round) &&
				!cs.LockedBlock.HashesTo(blockID.Hash) {

				cs.Logger.Info("Unlocking because of POL.", "lockedRound", cs.LockedRound, "POLRound", vote.Round)
				cs.LockedRound = -1
				cs.LockedBlock = nil
				cs.LockedBlockParts = nil
				cs.eventBus.PublishEventUnlock(cs.RoundStateEvent())
			}

			// Update Valid* if we can.
			// NOTE: our proposal block may be nil or not what received a polka..
			if len(blockID.Hash) != 0 && (cs.ValidRound < vote.Round) && (vote.Round == cs.Round) {

				if cs.ProposalBlock.HashesTo(blockID.Hash) {
					cs.Logger.Info(
						"Updating ValidBlock because of POL.", "validRound", cs.ValidRound, "POLRound", vote.Round)
					cs.ValidRound = vote.Round
					cs.ValidBlock = cs.ProposalBlock
					cs.ValidBlockParts = cs.ProposalBlockParts
				} else {
					cs.Logger.Info(
						"Valid block we don't know about. Set ProposalBlock=nil",
						"proposal", cs.ProposalBlock.Hash(), "blockId", blockID.Hash)
					// We're getting the wrong block.
					cs.ProposalBlock = nil
				}
				if !cs.ProposalBlockParts.HasHeader(blockID.PartsHeader) {
					cs.ProposalBlockParts = types.NewPartSetFromHeader(blockID.PartsHeader)
				}
				cs.evsw.FireEvent(types.EventValidBlock, &cs.RoundState)
				cs.eventBus.PublishEventValidBlock(cs.RoundStateEvent())
			}
		}

		// If +2/3 prevotes for *anything* for future round:
		switch {
		case cs.Round < vote.Round && prevotes.HasTwoThirdsAny():
			// Round-skip if there is any 2/3+ of votes ahead of us
			cs.enterNewRound(height, vote.Round)
		case cs.Round == vote.Round && cstypes.RoundStepPrevote <= cs.Step: // current round
			blockID, ok := prevotes.TwoThirdsMajority()
			if ok && (cs.isProposalComplete() || len(blockID.Hash) == 0) {
				cs.enterPrecommit(height, vote.Round)
			} else if prevotes.HasTwoThirdsAny() {
				cs.enterPrevoteWait(height, vote.Round)
			}
		case cs.Proposal != nil && 0 <= cs.Proposal.POLRound && cs.Proposal.POLRound == vote.Round:
			// If the proposal is now complete, enter prevote of cs.Round.
			if cs.isProposalComplete() {
				cs.enterPrevote(height, cs.Round)
			}
		}

	case types.PrecommitType:
		precommits := cs.Votes.Precommits(vote.Round)
		cs.Logger.Info("Added to precommit", "vote", vote, "precommits", precommits.StringShort())

		blockID, ok := precommits.TwoThirdsMajority()
		if ok {
			// Executed as TwoThirdsMajority could be from a higher round
			cs.enterNewRound(height, vote.Round)
			cs.enterPrecommit(height, vote.Round)
			if len(blockID.Hash) != 0 {
				cs.enterCommit(height, vote.Round)
				if cs.config.SkipTimeoutCommit && precommits.HasAll() {
					cs.enterNewRound(cs.Height, 0)
				}
			} else {
				cs.enterPrecommitWait(height, vote.Round)
			}
		} else if cs.Round <= vote.Round && precommits.HasTwoThirdsAny() {
			cs.enterNewRound(height, vote.Round)
			cs.enterPrecommitWait(height, vote.Round)
		}
	default:
		panic(fmt.Sprintf("Unexpected vote type %X", vote.Type)) // go-amino should prevent this.
	}

	return added, err
}

// sign the vote and publish on internalMsgQueue
func (cs *BLSConsensusState) signAddVote(type_ types.SignedMsgType, hash []byte, header types.PartSetHeader) *types.Vote {
	// if we don't have a key or we're not in the validator set, do nothing
	if cs.privValidator == nil || !cs.Validators.HasAddress(cs.privValidator.GetPubKey().Address()) {
		return nil
	}
	// If we don't have a verifier, do nothing.
	if cs.dkg.Verifier() == nil {
		return nil
	}

	var randomData []byte
	var err error
	if type_ == types.PrecommitType {
		randomData, err = cs.dkg.Verifier().Sign(cs.getPreviousBlock().Header.RandomData)
		if err != nil || len(randomData) == 0 {
			cs.Logger.Error("Error signing vote", "height", cs.Height, "round", cs.Round, "err", err,
				"type", type_, "hash", hash, "header", header, "random", randomData)
			return nil
		}
	}

	vote, err := cs.signVote(type_, hash, header, randomData)
	if err == nil {
		cs.sendInternalMessage(msgInfo{&VoteMessage{vote}, ""})
		cs.Logger.Info("Signed and pushed vote", "height", cs.Height, "round", cs.Round, "vote", vote, "err", err)
		return vote
	}
	//if !cs.replayMode {
	cs.Logger.Error("Error signing vote", "height", cs.Height, "round", cs.Round, "vote", vote, "err", err)
	//}
	return nil
}

func (cs *BLSConsensusState) signVote(type_ types.SignedMsgType, hash []byte, header types.PartSetHeader, data []byte) (*types.Vote, error) {
	// Flush the WAL. Otherwise, we may not recompute the same vote to sign, and the privValidator will refuse to sign anything.
	cs.wal.FlushAndSync()

	addr := cs.privValidator.GetPubKey().Address()
	valIndex, _ := cs.Validators.GetByAddress(addr)

	vote := &types.Vote{
		ValidatorAddress: addr,
		ValidatorIndex:   valIndex,
		Height:           cs.Height,
		Round:            cs.Round,
		Timestamp:        cs.voteTime(),
		Type:             type_,
		BlockID:          types.BlockID{Hash: hash, PartsHeader: header},
		BLSSignature:     data,
	}
	err := cs.privValidator.SignData(cs.state.ChainID, vote)
	return vote, err
}

func BLSWithDKG(dkg dkgtypes.DKG) BLSStateOption {
	return func(cs *BLSConsensusState) { cs.dkg = dkg }
}

func BLSWithEVSW(evsw tmevents.EventSwitch) BLSStateOption {
	return func(cs *BLSConsensusState) { cs.evsw = evsw }
}

// StateMetrics sets the metrics.
func BLSStateMetrics(metrics *Metrics) BLSStateOption {
	return func(cs *BLSConsensusState) { cs.metrics = metrics }
}

func (cs *BLSConsensusState) GetDKGMsgQueue() chan *dkgtypes.DKGDataMessage {
	return cs.dkg.MsgQueue()
}
