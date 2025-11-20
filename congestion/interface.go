package congestion

import (
	"time"

	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/graph/db/models"
	"github.com/lightningnetwork/lnd/lnwire"
)

// AccountableSignal indicates whether a peer should be held accountable for
// the resolution time of a HTLC.
type AccountableSignal bool

const (
	// Accountable indicates that the peer should be held accountable for
	// HTLC resolution time.
	Accountable AccountableSignal = true

	// Unaccountable indicates that the peer should not be held accountable
	// for HTLC resolution time.
	Unaccountable AccountableSignal = false
)

// ProposedHTLC contains the details of a HTLC that has been proposed for
// forwarding.
type ProposedHTLC struct {
	// AddedAt is the time the HTLC was added.
	AddedAt time.Time

        // AddedHeight is the block height that the HTLC was added at.
        AddedHeight uint32

	// FeeMsat is the fee in millisatoshis that will be earned by
	// forwarding this HTLC.
	FeeMsat lnwire.MilliSatoshi

	// IncomingExpiryHeight is the expiry height of the incoming HTLC.
	IncomingExpiryHeight uint32

	// IncomingCircuit is the circuit key of the incoming HTLC.
	IncomingCircuit models.CircuitKey

	// OutgoingChannel is the short channel ID of the outgoing channel.
	OutgoingChannel lnwire.ShortChannelID

	// IncomingAccountable is the received accountability signal for
	// the incoming HTLC.
	IncomingAccountable AccountableSignal
}

// ResourceManager maintains the current state of peers resource utilization
// and recommends forwarding actions for newly added HTLCs.
type ResourceManager interface {
	// HandleUpdateAddHTLC returns a signal indicating whether resources
	// can be allocated to the proposed outgoing HTLC. It returns None
	// when the HTLC should be dropped, and its proposed accountability
	// signal in Some when it should be forwarded.
	HandleUpdateAddHTLC(proposed ProposedHTLC) (fn.Option[bool], error)

	// HandleUpdateFulfillHTLC reports that the outgoing HTLC forward that
	// corresponds to the incomingCircuit provided has been successfully
	// fulfilled.
	HandleUpdateFulfillHTLC(resolvedAt time.Time,
		incomingCircuit models.CircuitKey) error

	// HandleUpdateFailHTLC reports that the outgoing HTLC forward that
	// corresponds to the incomingCircuit provided has been failed.
	HandleUpdateFailHTLC(resolvedAt time.Time,
		incomingCircuit models.CircuitKey) error
}

// A compile time check to ensure InactiveResourceManager implements the
// ResourceManager interface.
var _ ResourceManager = (*InactiveResourceManager)(nil)

// InactiveResourceManager is a no-op implementation of ResourceManager.
type InactiveResourceManager struct{}

// HandleUpdateAddHTLC is a no-op implementation of ResourceManager.
func (_ *InactiveResourceManager) HandleUpdateAddHTLC(
	_ ProposedHTLC) (fn.Option[bool], error) {

	return fn.None[bool](), nil
}

// HandleUpdateFulfillHTLC is a no-op implementation of ResourceManager.
func (_ *InactiveResourceManager) HandleUpdateFulfillHTLC(_ time.Time,
	_ models.CircuitKey) error {
	return nil
}

// HandleUpdateFailHTLC is a no-op implementation of ResourceManager.
func (_ *InactiveResourceManager) HandleUpdateFailHTLC(_ time.Time,
	_ models.CircuitKey) error {

	return nil
}
