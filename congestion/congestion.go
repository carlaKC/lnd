package congestion

import (
	"fmt"
	"sync"
	"time"

	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/graph/db/models"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ErrChangeInOnionValue is returns when a HTLC is reported to the manager with a
// different fee to what was originally proposed.
var ErrChangeInOnionValue = fmt.Errorf("change in fee for htlc")

// A compile time check to ensure ResourceManager implements the
// ResourceManager interface.
var _ ResourceManager = (*Manager)(nil)

// Manager implements reputation tracking and resource bucketing to limit
// our peers access to our resources, based on their historical behavior and
// currently in flight traffic.
type Manager struct {
	lock sync.RWMutex

	// inFlightByIncoming maps incoming circuit keys to their in-flight
	// data for quick lookups by incoming circuit.
	inFlightByIncoming map[models.CircuitKey]inFlightData
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
func NewManager() *Manager {
	return &Manager{
		inFlightByIncoming: make(map[models.CircuitKey]inFlightData),
	}
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

	m.inFlightByIncoming[proposed.IncomingCircuit] = inFlightData{
		addedAt:              proposed.AddedAt,
		addedHeight:          proposed.AddedHeight,
		feeMsat:              proposed.FeeMsat,
		incomingExpiryHeight: proposed.IncomingExpiryHeight,
		outgoingChannel:      proposed.OutgoingChannel,
		// TODO: choose outgoing value and set it here
		outgoingAccountable: proposed.IncomingAccountable,
	}

	return fn.Some(bool(proposed.IncomingAccountable)), nil
}

// HandleUpdateFulfillHTLC removes the in-flight HTLC from tracking when it
// is successfully fulfilled.
func (m *Manager) HandleUpdateFulfillHTLC(_ time.Time,
	incomingCircuit models.CircuitKey) error {

	m.lock.Lock()
	defer m.lock.Unlock()

	return m.removeInFlight(incomingCircuit)
}

// HandleUpdateFailHTLC removes the in-flight HTLC from tracking when it fails.
func (m *Manager) HandleUpdateFailHTLC(_ time.Time,
	incomingCircuit models.CircuitKey) error {

	m.lock.Lock()
	defer m.lock.Unlock()

	return m.removeInFlight(incomingCircuit)
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
func (m *Manager) removeInFlight(incomingCircuit models.CircuitKey) error {
	_, ok := m.inFlightByIncoming[incomingCircuit]
	if !ok {
		log.Debugf("Attempted to remove non-existent in-flight HTLC "+
			"for incoming circuit %v", incomingCircuit)
		return nil
	}
	delete(m.inFlightByIncoming, incomingCircuit)

	return nil
}
