package routing

import (
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
)

// BlindedPayment provides the path and payment parameters required to send a
// payment along a blinded path.
type BlindedPayment struct {
	// BlindedPath contains the unblinded introduction point and blinded
	// hops for the blinded section of the payment.
	BlindedPath *sphinx.BlindedPath

	// AggregateConstraints are the payment constraints for the full
	// blinded section of the route (ie, after the introduction node).
	AggregateConstraints *lnwire.PaymentConstraints

	// AggregateRelay are the aggregated relay parameters for the full
	// blinded section of the route (ie, after the introduction node).
	AggregateRelay *lnwire.PaymentRelay
}
