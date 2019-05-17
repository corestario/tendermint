package consensus

import (
	"errors"

	"github.com/tendermint/tendermint/libs/events"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
)

type DKGMockDontSendOneJustification struct {
	Dealer
	logger log.Logger
}

func NewDKGMockDealerNoJustification(validators *types.ValidatorSet, pv types.PrivValidator, sendMsgCb func(*types.DKGData) error, eventFirer events.Fireable, logger log.Logger, startRound int) Dealer {
	return &DKGMockDontSendOneJustification{NewDKGDealer(validators, pv, sendMsgCb, eventFirer, logger, startRound), logger}
}

func (m *DKGMockDontSendOneJustification) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendOneJustification) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.Dealer.SendDeals,
		m.Dealer.ProcessDeals,
		m.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendOneJustification) ProcessResponses() (error, bool) {
	if !m.Dealer.IsResponsesReady() {
		return nil, false
	}

	messages, err := m.GetJustifications()
	if err != nil {
		return err, true
	}

	messages = messages[1:]
	for _, msg := range messages {
		if err = m.Dealer.SendMsgCb(msg); err != nil {
			return err, true
		}
	}

	m.logger.Info("dkgState: sending justifications", "justifications", len(messages))

	return nil, true
}

func (m *DKGMockDontSendOneJustification) GetResponses() ([]*types.DKGData, error) {
	responses, err := m.Dealer.GetResponses()
	if len(responses) == 0 {
		return nil, errors.New("DKGMockDontSendOneJustification got empty Responses")
	}

	// remove one response message
	responses = responses[:len(responses)-1]

	return responses, err
}

type DKGMockDontSendAnyJustifications struct {
	Dealer
	logger log.Logger
}

func NewDKGMockDealerAnyJustifications(validators *types.ValidatorSet, pv types.PrivValidator, sendMsgCb func(*types.DKGData) error, eventFirer events.Fireable, logger log.Logger, startRound int) Dealer {
	return &DKGMockDontSendAnyJustifications{NewDKGDealer(validators, pv, sendMsgCb, eventFirer, logger, startRound), logger}
}

func (m *DKGMockDontSendAnyJustifications) Start() error {
	err := m.Dealer.Start()
	if err != nil {
		return err
	}
	m.GenerateTransitions()
	return nil
}

func (m *DKGMockDontSendAnyJustifications) GenerateTransitions() {
	m.Dealer.SetTransitions([]transition{
		// Phase I
		m.Dealer.SendDeals,
		m.Dealer.ProcessDeals,
		m.ProcessResponses,
		m.Dealer.ProcessJustifications,
		// Phase II
		m.Dealer.ProcessCommits,
		m.Dealer.ProcessComplaints,
		m.Dealer.ProcessReconstructCommits,
	})
}

func (m *DKGMockDontSendAnyJustifications) ProcessResponses() (error, bool) {
	if !m.Dealer.IsResponsesReady() {
		return nil, false
	}

	m.logger.Info("dkgState: sending justifications", "justifications", 0)

	return nil, true
}
