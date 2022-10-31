package routing

import (
	"github.com/go-errors/errors"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	errCltvDeltaSet = errors.New("final delta should be 0 when a blinded " +
		"path is supplied")

	errCltvLimitSet = errors.New("cltv limit should be 0 when a blinded " +
		"path is supplied")
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

// blindedPaymentParams updates the amount and final htlc expiry for a payment
// to account for additional hops that may be included in a blinded path. If
// no blinded path is passed in here, the function is a no-op.
func blindedPaymentParams(target route.Vertex, amount lnwire.MilliSatoshi,
	cltvLimit uint32, finalCltvDelta uint16, blindedPath *BlindedPayment) (
	route.Vertex, lnwire.MilliSatoshi, uint32, uint16, error) {

	if blindedPath == nil {
		return target, amount, cltvLimit, finalCltvDelta, nil
	}

	// If we have a blinded path, we don't expect cltv values to be set -
	// they should be obtained from the blinded path itself.
	if finalCltvDelta != 0 {
		return route.Vertex{}, 0, 0, 0, errCltvDeltaSet
	}

	if cltvLimit != 0 {
		return route.Vertex{}, 0, 0, 0, errCltvLimitSet
	}

	// If we have a blinded path, we need to include the base and
	// proportional fees for the blinded section in the final amount. This
	// is because (so far as pathfinding knows) we are just looking for a
	// route to the introduction node. We need to include the additional
	// fees/delay for the blinded portion, which we'll just forward to the
	// introduction node. Nodes in the blinded route will be responsible
	// for dividing up their fees/delay based on the information provided
	// to them by the recipient.
	proportionalFees := (uint32(amount) * blindedPath.AggregateRelay.FeeRate) / 1e6
	blindedTotal := amount + lnwire.MilliSatoshi(
		proportionalFees+blindedPath.AggregateRelay.BaseFee,
	)

	log.Infof("Payment to blinded path updated cltv limit/delta to: "+
		"%v/%v and payment amount to: %v (original amount: %v)",
		blindedPath.AggregateConstraints.MaxCltvExpiry,
		blindedPath.AggregateRelay.CltvExpiryDelta,
		blindedTotal, amount)

	return route.NewVertex(blindedPath.BlindedPath.IntroductionPoint),
		blindedTotal, blindedPath.AggregateConstraints.MaxCltvExpiry,
		blindedPath.AggregateRelay.CltvExpiryDelta, nil
}
