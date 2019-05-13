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
	"reflect"
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
	logger := consensusLogger().With("test", "byzantine")
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, nil)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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
		if handlers[i].Counter[types.EventDKGSuccessful] == 0 {
			t.Fatal("Node ", i, "hasn't finished dkg")
		}
	}
	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontSendOneDeal(t *testing.T) {
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoDeal})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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
	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontAnyDeals(t *testing.T) {
	t.SkipNow()
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyDeal})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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

	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontSendOneResponse(t *testing.T) {
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoResponse})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
			for i := range eventChans[j] {
				logger.Info("***got ", "node", j, "name", reflect.TypeOf(i).Name())
				wg.Done()
				n++
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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

	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontAnyResponses(t *testing.T) {
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyResponses})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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

	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontSendOneJustification(t *testing.T) {
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerNoJustification})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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

	fmt.Println("************************************ All is done")
}

func TestByzantineDKGDontAnyJustifications(t *testing.T) {
	N := 4
	logger := consensusLogger().With("test", "byzantine")
	dkgConstructor := NewDealerConstructor(map[int]DKGDealerConstructor{0: NewDKGMockDealerAnyJustifications})
	css := randConsensusNet(N, "consensus_byzantine_test", newMockTickerFunc(false), newCounter, dkgConstructor)

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
		for _, r := range reactors {
			r.(*ConsensusReactor).Switch.Stop()
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
				fmt.Printf("Validator %d got block %d of %d\n", j, n, blocksToWait)
				if n == blocksToWait {
					fmt.Printf("Validator %d got all %d blocks", j, n)
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

	fmt.Println("************************************ All is done")
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
		Counter:  make(map[string]int, 0),
		Handlers: make(map[string]events.EventCallback, 0),
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
