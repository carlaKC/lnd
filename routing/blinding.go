package routing

import (
	"fmt"

	"github.com/go-errors/errors"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	// ErrNoBlindedPath is returned when the blinded path in a blinded
	// payment is missing.
	ErrNoBlindedPath = errors.New("blinded path required")

	// ErrNoPaymentInfo is returned when the payment info in a blinded
	// payment is missing.
	ErrNoPaymentInfo = errors.New("payment info required")

	// ErrInsufficientBlindedHops is returned when a blinded path does
	// not have enough blinded hops.
	ErrInsufficientBlindedHops = errors.New("blinded path requires " +
		"at least two hops")
)

// BlindedPayment provides the path and payment parameters required to send a
// payment along a blinded path.
type BlindedPayment struct {
	// BlindedPath contains the unblinded introduction point and blinded
	// hops for the blinded section of the payment.
	BlindedPath *sphinx.BlindedPath

	// PaymentInfo contains the relay parameters and restrictions for the
	// payment to the path provided.
	PaymentInfo *lnwire.PaymentInfo
}

// Validate performs validation on a blinded payment.
func (b *BlindedPayment) Validate() error {
	if b.BlindedPath == nil {
		return ErrNoBlindedPath
	}

	if b.PaymentInfo == nil {
		return ErrNoPaymentInfo
	}

	// The sphinx library inserts the introduction node as the first hop,
	// so we expect at least two hops (an introduction node on its own
	// makes no sense).
	if len(b.BlindedPath.BlindedHops) < 2 { //nolint:gomnd
		return fmt.Errorf("%w got: %v", ErrInsufficientBlindedHops,
			len(b.BlindedPath.BlindedHops))
	}

	return nil
}

// toRouteHints produces a set of chained route hints that represent a blinded
// path. This function assumes that we have a valid blinded path (one with at
// least two blinded hops / encrypted data blob).
func (b *BlindedPayment) toRouteHints() map[route.Vertex][]*channeldb.CachedEdgePolicy { //nolint:lll
	hintCount := len(b.BlindedPath.BlindedHops) - 1
	hints := make(
		map[route.Vertex][]*channeldb.CachedEdgePolicy, hintCount,
	)

	// Start at the unblinded introduction node, because our pathfinding
	// will be able to locate this point in the graph.
	fromNode := route.NewVertex(b.BlindedPath.IntroductionPoint)

	features := lnwire.EmptyFeatureVector()
	if b.PaymentInfo.Features != nil {
		features = b.PaymentInfo.Features.Clone()
	}

	// Use the total aggregate relay parameters for the entire blinded
	// route as the policy for the hint from our introduction node. This
	// will ensure that pathfinding provides sufficient fees/delay for the
	// blinded portion to the introduction node.
	hints[fromNode] = []*channeldb.CachedEdgePolicy{
		{
			TimeLockDelta: b.PaymentInfo.CltvExpiryDelta,
			MinHTLC:       b.PaymentInfo.HtlcMinimumMsat,
			MaxHTLC:       b.PaymentInfo.HtlcMaximumMsat,
			FeeBaseMSat: lnwire.MilliSatoshi(
				b.PaymentInfo.BaseFee,
			),
			FeeProportionalMillionths: lnwire.MilliSatoshi(
				b.PaymentInfo.FeeRate,
			),
			ToNodePubKey: func() route.Vertex {
				return route.NewVertex(
					// The first node in this slice is
					// the introduction node, so we start
					// at index 1 to get the first blinded
					// relaying node.
					b.BlindedPath.BlindedHops[1].NodePub,
				)
			},
			ToNodeFeatures: features,
		},
	}

	// Start at an offset of 1 because the first node in our blinded hops
	// is the introduction node and terminate at the second-last node
	/// because we're dealing with hops as pairs.
	for i := 1; i < hintCount; i++ {
		// Set our origin node to the current
		fromNode = route.NewVertex(
			b.BlindedPath.BlindedHops[i].NodePub,
		)

		// Create a hint which has no fee or cltv delta. We
		// specifically want zero values here because our relay
		// parameters are expressed in encrypted blobs rather than the
		// route itself for blinded routes.
		nextHopIdx := i + 1
		nextNode := route.NewVertex(
			b.BlindedPath.BlindedHops[nextHopIdx].NodePub,
		)

		hint := &channeldb.CachedEdgePolicy{
			ToNodePubKey: func() route.Vertex {
				return nextNode
			},
		}

		hints[fromNode] = []*channeldb.CachedEdgePolicy{
			hint,
		}
	}

	return hints
}
