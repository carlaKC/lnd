package congestion

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/clock"
	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/graph/db/models"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ErrChangeInOnionValue is returns when a HTLC is reported to the manager with a
// different fee to what was originally proposed.
var ErrChangeInOnionValue = fmt.Errorf("change in fee for htlc")

// ErrInvalidMsat is returned when a millisatoshi value cannot be safely
// converted to int64.
var ErrInvalidMsat = fmt.Errorf("msat value out of valid range")

// ErrInvalidHoldTime is returned is a htlc is resolved with a zero or negative
// hold time, which is not practically possible.
var ErrInvalidHoldTime = fmt.Errorf("hold time less than or equal to zero")

// ErrExpiringHTLC is returned when a htlc is presented that is expiring on
// the incoming link. We do not expect to forward expiring HTLCs, as there is
// risk of loss of funds.
var ErrExpiringHTLC = fmt.Errorf("incoming htlc is expiring")

// InFlightHTLC describes a forwarded HTLC that is currently irrevocably
// committed on the outgoing channel, and has not yet been settled or failed.
type InFlightHTLC struct {
	// InKey is the circuit key identifying the incoming channel.
	InKey models.CircuitKey
	// OutKey is the circuit key identifying the outgoing channel.
	OutKey models.CircuitKey
	// FeeMsat paid to forward the HTLC.
	FeeMsat lnwire.MilliSatoshi
}

type Config struct {
	// ReputationParams set the parameters for revenue and reputation
	// tracking.
	ReputationParams *ReputationParams

	// Fetches all currently open channels from the database.
	// TODO: what happens if a HTLC is on a closed channel?
	FetchAllOpenChannels func() ([]*channeldb.OpenChannel, error)

	// ListOpenCircuits returns a list of all the currently in-flight
	// HTLCs forwarded by our node.
	ListOpenCircuits func() []InFlightHTLC

	// Clock provides wall time, and allows deterministic tests.
	Clock clock.Clock
}

// A compile time check to ensure ResourceManager implements the
// ResourceManager interface.
var _ ResourceManager = (*Manager)(nil)

// ReputationParams contains the parameters for reputation tracking and
// resource management.
type ReputationParams struct {
	// RevenueWindow is the period of time that revenue should be tracked
	// to determine the threshold for reputation decisions.
	RevenueWindow time.Duration

	// ReputationMultiplier is the multiplier applied to RevenueWindow that
	// determines the period that reputation is built over.
	ReputationMultiplier uint8

	// ResolutionPeriod is the threshold above which HTLCs will be
	// penalized for slow resolution.
	ResolutionPeriod time.Duration

	// ExpectedBlockSpeed is the expected block speed, surfaced to allow
	// test networks to set different durations.
	ExpectedBlockSpeed time.Duration
}

func (r *ReputationParams) repuationWindow() time.Duration {
	return r.RevenueWindow * time.Duration(r.ReputationMultiplier)
}

// opportunityCost calculates the opportunity cost of holding a HTLC based on
// its fee and hold time.
func (r *ReputationParams) opportunityCost(feeMsat lnwire.MilliSatoshi,
	holdTime time.Duration) uint64 {

	// We use a float to calculate the number of periods that a htlc is
	// held for so that we have a linear increase in cost.
	periods := float64(holdTime) / float64(r.ResolutionPeriod)

	// Multiply by fee, checking for overflow.
	result := periods * float64(feeMsat)
	if result > float64(math.MaxUint64) {
		return math.MaxUint64
	}

	return uint64(result)
}

// effectiveFees calculates the fee contribution of a HTLC based on its hold
// time, accountability, and resolution. Returns the effective fees as an int64
// which may be negative if the opportunity cost for holding the HTLC exceeds
// paid fees.
func (r *ReputationParams) effectiveFees(feeMsat lnwire.MilliSatoshi,
	holdTime time.Duration, accountable AccountableSignal,
	settled bool) (int64, error) {

	// Fees paid for the HTLC contribute to effectived fees.
	var paidFees int64
	if settled {
		if feeMsat > math.MaxInt64 {
			return 0, fmt.Errorf("%w: %d", ErrInvalidMsat, feeMsat)
		}
		paidFees = int64(feeMsat)
	}

	// Calculate opportunity cost and convert to int64.
	oppCost := r.opportunityCost(feeMsat, holdTime)
	var oppCostInt64 int64
	if oppCost > math.MaxInt64 {
		oppCostInt64 = math.MaxInt64
	} else {
		oppCostInt64 = int64(oppCost)
	}

	var effectiveFees int64
	if oppCostInt64 > 0 && paidFees < math.MinInt64+oppCostInt64 {
		effectiveFees = math.MinInt64
	} else {
		effectiveFees = paidFees - oppCostInt64
	}

	// Unaccountable HTLCs do not have a negative impact on reputation.
	if accountable == Unaccountable && effectiveFees <= 0 {
		return 0, nil
	}

	return effectiveFees, nil
}

// Manager implements reputation tracking and resource bucketing to limit
// our peers access to our resources, based on their historical behavior and
// currently in flight traffic.
type Manager struct {
	lock sync.RWMutex

	params *ReputationParams

	// inFlightByIncoming maps incoming circuit keys to their in-flight
	// data for quick lookups by incoming circuit.
	inFlightByIncoming map[models.CircuitKey]inFlightData

	// incomingChannelRevenue tracks the total revenue that an incoming
	// link has been responsible for forwarding use over the RevenueWindow
	// in our ReputationParams.
	incomingChannelRevenue map[lnwire.ShortChannelID]*RevenueAverage

	// outgoingChannelReputation tracks the reputation that channels
	// have earned over ReputationParams.reputationWindow() for successfully
	// and quickly resolving HTLCs forwarded out over the channel.
	outgoingChannelReputation map[lnwire.ShortChannelID]int64
}

type inFlightData struct {
	addedAt              time.Time
	addedHeight          uint32
	feeMsat              lnwire.MilliSatoshi
	incomingExpiryHeight uint32
	outgoingChannel      lnwire.ShortChannelID
	outgoingAccountable  AccountableSignal
}

// NewManager creates a new congestion Manager with initialized maps.
func NewManager(cfg *Config, startHeight uint32) (*Manager, error) {
	inFlightByIncoming := make(map[models.CircuitKey]inFlightData)

	incomingHTLCs := make(map[models.CircuitKey]*channeldb.HTLC)

	channels, err := cfg.FetchAllOpenChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch open channels: %w", err)
	}

	for _, channel := range channels {
		chanID := channel.ShortChanID()
		htlcs := channel.ActiveHtlcs()
		for _, htlc := range htlcs {
			if !htlc.Incoming {
				continue
			}
			circuitKey := models.CircuitKey{
				ChanID: chanID,
				HtlcID: htlc.HtlcIndex,
			}

			incomingHTLCs[circuitKey] = &htlc
		}
	}

	for _, circuit := range cfg.ListOpenCircuits() {
		htlc, ok := incomingHTLCs[circuit.InKey]
		if !ok {
			// TODO(CKC): is it possible that we can't find a htlc
			// in our channels if it is in the circuit map?
			log.Warnf("HTLC: %v(%v) -> %v(%v) found in circuit "+
				"map but not on incoming channel",
				circuit.InKey.ChanID, circuit.InKey.HtlcID,
				circuit.OutKey.ChanID, circuit.OutKey.HtlcID)
			continue
		}

		accountable := Unaccountable
		accountableType := uint64(lnwire.ExperimentalAccountableType)
		accBytes, ok := htlc.CustomRecords[accountableType]
		if ok && len(accBytes) > 0 {
			if accBytes[0] == lnwire.ExperimentalAccountable {
				accountable = Accountable
			}
		}

		inFlightByIncoming[circuit.InKey] = inFlightData{
			// TODO(CKC): need to be able to recover timestamp,
			// from that we can "best effort" restore the
			// addedHeight using 10 minute blocks.
			addedAt:              cfg.Clock.Now(),
			addedHeight:          startHeight,
			feeMsat:              circuit.FeeMsat,
			incomingExpiryHeight: htlc.RefundTimeout,
			outgoingChannel:      circuit.OutKey.ChanID,
			outgoingAccountable:  accountable,
		}
	}

	return &Manager{
		params:                    cfg.ReputationParams,
		inFlightByIncoming:        inFlightByIncoming,
		incomingChannelRevenue:    make(map[lnwire.ShortChannelID]*RevenueAverage),
		outgoingChannelReputation: make(map[lnwire.ShortChannelID]int64),
	}, nil
}

// HandleUpdateAddHTLC notifies the manager that an UpdateAddHTLC that
// originates on the incomingCircuit has been proposed for forwarding on the
// outgoingChannel. This method allows replays for the same incomingCircuit.
// It will fail if values fixed in the onion (such as feeMsat) change, but
// allows routing changes such as choice of outgoingChannel.
func (m *Manager) HandleUpdateAddHTLC(proposed ProposedHTLC) (fn.Option[bool],
	error) {

	m.lock.Lock()
	defer m.lock.Unlock()

	inFlight, err := m.getInFlight(
		proposed.AddedAt, proposed.FeeMsat, proposed.IncomingExpiryHeight,
		proposed.IncomingCircuit, proposed.OutgoingChannel,
	)
	if err != nil {
		return fn.None[bool](), err
	}

	if inFlight.IsSome() {
		return fn.Some(bool(
			inFlight.UnsafeFromSome().outgoingAccountable,
		)), nil
	}

	hasReputation, err := m.hasReputation(
		proposed.AddedAt, proposed.AddedHeight, proposed.FeeMsat,
		proposed.IncomingExpiryHeight, proposed.IncomingCircuit.ChanID,
		proposed.OutgoingChannel,
	)
	if err != nil {
		return fn.None[bool](), err
	}

	m.inFlightByIncoming[proposed.IncomingCircuit] = inFlightData{
		addedAt:              proposed.AddedAt,
		addedHeight:          proposed.AddedHeight,
		feeMsat:              proposed.FeeMsat,
		incomingExpiryHeight: proposed.IncomingExpiryHeight,
		outgoingChannel:      proposed.OutgoingChannel,
		outgoingAccountable:  AccountableSignal(hasReputation),
	}

	return fn.Some(hasReputation), nil
}

// HandleUpdateFulfillHTLC removes the in-flight HTLC from tracking when it
// is successfully fulfilled.
func (m *Manager) HandleUpdateFulfillHTLC(resolvedAt time.Time,
	incomingCircuit models.CircuitKey) error {

	m.lock.Lock()
	defer m.lock.Unlock()

	return m.removeInFlight(incomingCircuit, resolvedAt, true)
}

// HandleUpdateFailHTLC removes the in-flight HTLC from tracking when it fails.
func (m *Manager) HandleUpdateFailHTLC(resolvedAt time.Time,
	incomingCircuit models.CircuitKey) error {

	m.lock.Lock()
	defer m.lock.Unlock()

	return m.removeInFlight(incomingCircuit, resolvedAt, false)
}

// Note: must be called with m.Lock held.
func (m *Manager) hasReputation(addedAt time.Time, addedHeight uint32,
	feeMsat lnwire.MilliSatoshi, incomingExpiryHeight uint32,
	incomingChannel lnwire.ShortChannelID,
	outgoingChannel lnwire.ShortChannelID) (bool, error) {

	if incomingExpiryHeight <= addedHeight {

	}

	var incomingRevenue int64 = 0
	if revenue := m.incomingChannelRevenue[incomingChannel]; revenue != nil {
		var err error
		incomingRevenue, err = revenue.ValueAtInstant(addedAt)
		if err != nil {
			return false, err
		}
	}

	reputation := m.outgoingChannelReputation[outgoingChannel]

	worstHoldTime := time.Duration(
		incomingExpiryHeight-addedHeight,
	) * m.params.ExpectedBlockSpeed
	htlcRisk := m.params.opportunityCost(feeMsat, worstHoldTime)

	return reputation > incomingRevenue+int64(htlcRisk), nil
}

// getInFlight returns the existing records of a HTLC if it is currently stored.
// It fails if the newly reported HTLC has different fee or expiry to the
// previous one, as this should be fixed in the onion, but allows changes in
// outgoing channel to accommodate non-strict forwarding.
//
// Note: must be called with m.Lock held.
func (m *Manager) getInFlight(addedAt time.Time,
	feeMsat lnwire.MilliSatoshi, incomingExpiryHeight uint32,
	incomingCircuit models.CircuitKey,
	outgoingChannel lnwire.ShortChannelID) (fn.Option[inFlightData], error) {

	// Check if this HTLC is already tracked. If so, validate that the
	// onion-fixed parameters haven't changed.
	old, exists := m.inFlightByIncoming[incomingCircuit]
	if !exists {
		// HTLC not found, return None so caller can add it.
		return fn.None[inFlightData](), nil
	}

	if old.feeMsat != feeMsat {
		return fn.None[inFlightData](), fmt.Errorf("%w: fee "+
			"was: %v now: %v", ErrChangeInOnionValue,
			old.feeMsat, feeMsat)
	}

	if old.incomingExpiryHeight != incomingExpiryHeight {
		return fn.None[inFlightData](), fmt.Errorf("%w expiry "+
			"was: %v now: %v", ErrChangeInOnionValue,
			old.incomingExpiryHeight, incomingExpiryHeight)
	}

	if old.outgoingChannel != outgoingChannel {
		log.Warnf("Duplicate add for incoming circuit %v: old "+
			"outgoing=%v, new outgoing=%v", incomingCircuit,
			old.outgoingChannel, outgoingChannel)
	}

	// HTLC exists with matching parameters, return it.
	return fn.Some(old), nil
}

// removeInFlight idempotently removes an in-flight HTLC from Manager.
//
// Note: must be called with m.Lock held.
func (m *Manager) removeInFlight(incomingCircuit models.CircuitKey,
	resolvedAt time.Time, settled bool) error {

	inFlight, ok := m.inFlightByIncoming[incomingCircuit]
	if !ok {
		log.Debugf("Attempted to remove non-existent in-flight HTLC "+
			"for incoming circuit %v", incomingCircuit)
		return nil
	}
	delete(m.inFlightByIncoming, incomingCircuit)

	// Any successful htlc contributes to the incoming channel's revenue.
	if settled {
		revenueAvg, ok := m.incomingChannelRevenue[incomingCircuit.ChanID]
		if !ok {
			var err error
			revenueAvg, err = NewRevenueAverage(
				m.params.RevenueWindow,
				m.params.ReputationMultiplier,
				resolvedAt,
				fn.None[int64](),
			)
			if err != nil {
				return fmt.Errorf("failed to create revenue "+
					"average for channel %v: %w",
					incomingCircuit.ChanID, err)
			}
			m.incomingChannelRevenue[incomingCircuit.ChanID] = revenueAvg
		}

		if _, err := revenueAvg.AddValue(
			int64(inFlight.feeMsat), resolvedAt,
		); err != nil {
			return fmt.Errorf("failed to add revenue value for "+
				"channel %v: %w", incomingCircuit.ChanID, err)
		}
	}

	holdTime := resolvedAt.Sub(inFlight.addedAt)
	if holdTime <= 0 {
		return fmt.Errorf("%w: added at: %v removed at: %v",
			ErrInvalidHoldTime, inFlight.addedAt, resolvedAt)
	}

	effectiveFees, err := m.params.effectiveFees(
		inFlight.feeMsat, holdTime, inFlight.outgoingAccountable, settled,
	)
	if err != nil {
		return fmt.Errorf("%w: fee: %v with hold time: %v",
			err, inFlight.feeMsat, holdTime)
	}

	m.outgoingChannelReputation[inFlight.outgoingChannel] += effectiveFees
	return nil
}
