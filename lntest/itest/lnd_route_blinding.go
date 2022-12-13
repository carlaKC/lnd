package itest

import (
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
)

// PaymentRelay describes the relay policy for a blinded hop.
type PaymentRelay struct {
	// CltvExpiryDelta is the expiry delta for the payment hop.
	CltvExpiryDelta uint16

	// BaseFee is the per-htlc fee charged.
	BaseFee uint32

	// FeeRate is the fee rate that will be charged per millionth of a
	// satoshi.
	FeeRate uint32
}

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
	AggregateRelay *lnwire.PaymentRelayInfo
}
