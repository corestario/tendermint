package consensus

//type StateInterface interface {
//	GetState() sm.State
//	GetValidators() (int64, []*types.Validator)
//	GetLastHeight() int64
//	GetRoundStateJSON() ([]byte, error)
//	GetRoundStateSimpleJSON() ([]byte, error)
//	Start() error
//	Stop() error
//	Wait()
//	ReconstructLastCommit(sm.State)
//	GetMtx() *sync.RWMutex
//	GetVotes() *types2.HeightVoteSet
//	GetHeight() int64
//	GetLastCommit() *types.VoteSet
//	SetEventBus(*types.EventBus)
//	GetRoundState() *types2.RoundState
//	LoadCommit(int64) *types.Commit
//	Quit() <-chan struct{}
//	StringIndented(string) string
//	UpdateToState(state sm.State)
//	SetDoWALCatchup(bool)
//	GetEventSwitch() events.EventSwitch
//	GetPeerMsgQueue() chan msgInfo
//	GetStatsMsgQueue() chan msgInfo
//	GetBlockStore() state.BlockStore
//	GetConfig() *cfg.ConsensusConfig
//	GetDKGMsgQueue() chan *dkgtypes.DKGDataMessage
//	SetVerifier(dkgtypes.Verifier)
//}
