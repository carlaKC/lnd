package routing

import (
	"errors"
	"fmt"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
)

var (
	// ErrNoBlindedPath is returned when the blinded path in a blinded
	// payment is missing.
	ErrNoBlindedPath = errors.New("blinded path required")

	// ErrNoRelayInfo is returned when the payment info in a blinded
	// payment is missing.
	ErrNoRelayInfo = errors.New("relay info required")

	// ErrNoConstraints is returned when the constraints for a blinded
	// payment are missing.
	ErrNoConstraints = errors.New("constraints required")

	// ErrInsufficientBlindedHops is returned when a blinded path does
	// not have enough blinded hops.
	ErrInsufficientBlindedHops = errors.New("blinded path requires " +
		"at least two hops")
)

// AggregateRelay represents the aggregate payment relay parameters for a
// blinded payment.
type AggregateRelay record.PaymentRelayInfo

// AggregateConstraints represents the aggregate constraints for a blinded
// payment.
type AggregateConstraints record.PaymentConstraints

// BlindedPayment provides the path and payment parameters required to send a
// payment along a blinded path.
type BlindedPayment struct {
	// BlindedPath contains the unblinded introduction point and blinded
	// hops for the blinded section of the payment.
	BlindedPath *sphinx.BlindedPath

	// RelayInfo contains the relay parameters for payment to a blinded
	// path.
	RelayInfo *AggregateRelay

	// Constraints is a set of constraints that apply to the blinded
	// portion of the path.
	Constraints *AggregateConstraints

	// Features is the set of features required for the payment.
	Features *lnwire.FeatureVector
}

// Validate performs validation on a blinded payment.
func (b *BlindedPayment) Validate() error {
	if b.BlindedPath == nil {
		return ErrNoBlindedPath
	}

	if b.RelayInfo == nil {
		return ErrNoRelayInfo
	}

	if b.Constraints == nil {
		return ErrNoConstraints
	}

	// The sphinx library inserts the introduction node as the first hop,
	// so we expect at least one hop.
	if len(b.BlindedPath.BlindedHops) < 1 {
		return fmt.Errorf("%w got: %v", ErrInsufficientBlindedHops,
			len(b.BlindedPath.BlindedHops))
	}

	return nil
}
