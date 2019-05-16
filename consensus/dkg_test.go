package consensus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/types"
	"strconv"
)

func init() {
	config = ResetConfig("consensus_dkg_test")
}

const blocksToWait = 12
const timeToWait = 2 * blocksToWait * time.Second

var DKGEvents = []string{
	types.EventDKGStart,
	types.EventDKGSuccessful,
	types.EventDKGStart,
	types.EventDKGPubKeyReceived,
	types.EventDKGDealsProcessed,
	types.EventDKGResponsesProcessed,
	types.EventDKGJustificationsProcessed,
	types.EventDKGInstanceCertified,
	types.EventDKGCommitsProcessed,
	types.EventDKGComplaintProcessed,
	types.EventDKGReconstructCommitsProcessed,
	types.EventDKGSuccessful,
	types.EventDKGKeyChange,
}

func TestByzantineDKG(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, nil, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	handlers := MakeNDKGEventHandlers(N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)
		handlers[i].Subscribe(conR.conS.evsw)
		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("reactor closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log("Consensus Reactor", "index", i, "reactor", reactor)
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}

	for i := range handlers {
		t.Log(handlers[i].Counter)
		if handlers[i].Counter[types.EventDKGSuccessful] == 0 {
			t.Fatal("Node ", i, "hasn't finished dkg")
		}
	}
}

func TestByzantineDKGDontSendOneDeal(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoDeal})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	handlers := MakeNDKGEventHandlers(N)

	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR
		handlers[i].Subscribe(conR.conS.evsw)

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}

	for i := range handlers {
		if handlers[i].Counter[types.EventDKGSuccessful] > 0 {
			t.Fatal("Node ", i, "must be failed")
		}
	}
}

func TestByzantineDKGDontAnyDeals(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyDeal})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log("Consensus Reactor", "index", i, "reactor", reactor)
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontSendOneResponse(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoResponse})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)
		conR.conS.evsw.AddListenerForEvent("test", types.EventDKGStart, func(data events.EventData) {
			t.Log("Event received", data)
		})
		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	const blocksToWait = 11
	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontAnyResponses(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyResponses})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontSendOneJustification(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoJustification})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontAnyJustifications(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyJustifications})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontSendOneCommit(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoCommit})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func TestByzantineDKGDontAnyCommits(t *testing.T) {
	N := 4
	T := 3
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyCommits})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor, GetVerifier(T, N))

	switches := make([]*p2p.Switch, N)
	p2pLogger := logger.With("module", "p2p")
	for i := 0; i < N; i++ {
		switches[i] = p2p.MakeSwitch(
			config.P2P,
			i,
			"foo", "1.0.0",
			func(i int, sw *p2p.Switch) *p2p.Switch {
				return sw
			})
		switches[i].SetLogger(p2pLogger.With("validator", i))
	}

	eventChans := make([]chan interface{}, N)
	reactors := make([]p2p.Reactor, N)
	for i := 0; i < N; i++ {
		eventBus := css[i].eventBus
		eventBus.SetLogger(logger.With("module", "events", "validator", i))

		eventChans[i] = make(chan interface{}, 1)
		err := eventBus.Subscribe(context.Background(), testSubscriber, types.EventQueryNewBlock, eventChans[i])
		require.NoError(t, err)

		conR := NewConsensusReactor(css[i], true) // so we dont start the consensus states
		conR.SetLogger(logger.With("validator", i))
		conR.SetEventBus(eventBus)

		var conRI p2p.Reactor // nolint: gotype, gosimple
		conRI = conR

		reactors[i] = conRI
	}

	defer func() {
		for i, r := range reactors {
			if err := r.(*ConsensusReactor).Switch.Stop(); err != nil {
				logger.Error("event bus closed with error", "index", i, "err", err)
			}
		}
	}()

	p2p.MakeConnectedSwitches(config.P2P, N, func(i int, s *p2p.Switch) *p2p.Switch {
		// ignore new switch s, we already made ours
		switches[i].AddReactor("CONSENSUS", reactors[i])
		return switches[i]
	}, func(sws []*p2p.Switch, i, j int) {
		p2p.Connect2Switches(sws, i, j)
	})

	// start the non-byz state machines.
	// note these must be started before the byz
	for i := 0; i < N; i++ {
		cr := reactors[i].(*ConsensusReactor)
		cr.SwitchToConsensus(cr.conS.GetState(), 0)
	}

	wg := new(sync.WaitGroup)
	wg.Add(blocksToWait * N)
	for i := 0; i < N; i++ {
		go func(j int) {
			n := 0
			for range eventChans[j] {
				wg.Done()
				n++
				logger.Info("Validator got block", "validatorIndex", j, "blockNumber", n, "totalBlocks", blocksToWait)
				if n == blocksToWait {
					logger.Info("Validator got all blocks", "validatorIndex", j, "totalBlocks", n)
					break
				}
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	tick := time.NewTicker(timeToWait)
	select {
	case <-done:
	case <-tick.C:
		for i, reactor := range reactors {
			t.Log(fmt.Sprintf("Consensus Reactor %v", i))
			t.Log(fmt.Sprintf("%v", reactor))
		}
		t.Errorf("Timed out waiting for all validators to commit first block")
	}
}

func MakeNDKGEventHandlers(n int) []*dkgEventHandler {
	eh := make([]*dkgEventHandler, n)
	for i := 0; i < n; i++ {
		eh[i] = NewDkgEventHandler("handler_" + strconv.Itoa(i))
	}
	return eh
}
func NewDkgEventHandler(name string) *dkgEventHandler {
	return &dkgEventHandler{
		Name:     name,
		Counter:  make(map[string]int),
		Handlers: make(map[string]events.EventCallback),
	}
}

type dkgEventHandler struct {
	Name     string
	Counter  map[string]int
	Handlers map[string]events.EventCallback
}

func (eh *dkgEventHandler) Subscribe(evsw events.EventSwitch) {
	for _, e := range DKGEvents {
		event := e
		evsw.AddListenerForEvent(eh.Name, e, func(data events.EventData) {
			eh.Counter[event]++
			if h, ok := eh.Handlers[e]; ok {
				h(data)
			}
		})
	}
}

func createDKGMsg(addr []byte, roundID int, data []byte, toIndex, numEntities int) DKGDataMessage {
	return DKGDataMessage{
		&types.DKGData{
			Type:        types.DKGDeal,
			Addr:        addr,
			RoundID:     roundID,
			Data:        data,
			ToIndex:     toIndex,
			NumEntities: numEntities,
		},
	}
}

// TestDKGDataSignable test
func TestDKGDataSignable(t *testing.T) {
	var (
		expected, signBytes []byte
		err                 error
	)
	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)

	if signBytes, err = msg.Data.SignBytes(); err != nil {
		t.Error(err.Error())
		return
	}

	msg.Data.Signature = nil
	if expected, err = cdc.MarshalBinaryLengthPrefixed(msg.Data); err != nil {
		t.Error(err.Error())
		return
	}
	require.Equal(t, expected, signBytes, "Got unexpected sign bytes for DKGData.")
}

func TestDKGVerifyMessage(t *testing.T) {
	privVal := types.NewMockPV()

	// pubKey for dealer's address
	pubkey := privVal.GetPubKey()

	validator := types.NewValidator(pubkey, 10)
	validators := types.NewValidatorSet([]*types.Validator{validator})

	dealer := NewDKGDealer(validators, privVal, nil, nil, consensusLogger().With("test", "byzantine"))
	testAddr := []byte("some_test_address")
	testData := []byte("some_test_data")

	msg := createDKGMsg(testAddr, 1, testData, 1, 1)
	err := privVal.SignDKGData(msg.Data)
	require.NoError(t, nil, err)
	require.NoError(t, nil, dealer.VerifyMessage(msg))
}
