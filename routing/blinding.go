package routing

import (
	"errors"
	"fmt"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	// ErrNoBlindedPath is returned when the blinded path in a blinded
	// payment is missing.
	ErrNoBlindedPath = errors.New("blinded path required")

	// ErrInsufficientBlindedHops is returned when a blinded path does
	// not have enough blinded hops.
	ErrInsufficientBlindedHops = errors.New("blinded path requires " +
		"at least one hop")
)

// BlindedPayment provides the path and payment parameters required to send a
// payment along a blinded path.
type BlindedPayment struct {
	// BlindedPath contains the unblinded introduction point and blinded
	// hops for the blinded section of the payment.
	BlindedPath *sphinx.BlindedPath

	// BaseFee is the total base fee to be paid for payments made over the
	// blinded path.
	BaseFee uint32

	// ProportionalFee is the aggregated proportional fee for payments
	// made over the blinded path.
	ProportionalFee uint32

	// CltvExpiryDelta is the total expiry delta for payments made over
	// the blinded path.
	CltvExpiryDelta uint16

	// HtlcMinimum is the highest HLTC minimum supported along the blinded
	// path (while some hops may have lower values, we're effectively
	// bounded by the highest minimum).
	HtlcMinimum uint64

	// HtlcMaximum is the lowest HTLC maximum supported along the blinded
	// path (while some hops may have higher values, we're effectively
	// bounded by the lowest maximum).
	HtlcMaximum uint64

	// Features is the set of features required for the payment.
	Features *lnwire.FeatureVector
}

// Validate performs validation on a blinded payment.
func (b *BlindedPayment) Validate() error {
	if b.BlindedPath == nil {
		return ErrNoBlindedPath
	}

	// The sphinx library inserts the introduction node as the first hop,
	// so we expect at least one hop.
	if len(b.BlindedPath.BlindedHops) < 1 {
		return fmt.Errorf("%w got: %v", ErrInsufficientBlindedHops,
			len(b.BlindedPath.BlindedHops))
	}

	return nil
}

// toRouteHints produces a set of chained route hints that represent a blinded
// path.
func (b *BlindedPayment) toRouteHints() map[route.Vertex][]*channeldb.CachedEdgePolicy { //nolint:lll
	// If we just have a single hop in our blinded route, it just contains
	// an introduction node (this is a valid path according to the spec).
	// Since we have the un-blinded node ID for the introduction node, we
	// don't need to add any route hints.
	if len(b.BlindedPath.BlindedHops) <= 1 {
		return nil
	}

	hintCount := len(b.BlindedPath.BlindedHops) - 1
	hints := make(
		map[route.Vertex][]*channeldb.CachedEdgePolicy, hintCount,
	)

	// Start at the unblinded introduction node, because our pathfinding
	// will be able to locate this point in the graph.
	fromNode := route.NewVertex(b.BlindedPath.IntroductionPoint)

	features := lnwire.EmptyFeatureVector()
	if b.Features != nil {
		features = b.Features.Clone()
	}

	// Use the total aggregate relay parameters for the entire blinded
	// route as the policy for the hint from our introduction node. This
	// will ensure that pathfinding provides sufficient fees/delay for the
	// blinded portion to the introduction node.
	hints[fromNode] = []*channeldb.CachedEdgePolicy{
		{
			TimeLockDelta: b.CltvExpiryDelta,
			MinHTLC:       lnwire.MilliSatoshi(b.HtlcMinimum),
			MaxHTLC:       lnwire.MilliSatoshi(b.HtlcMaximum),
			FeeBaseMSat: lnwire.MilliSatoshi(
				b.BaseFee,
			),
			FeeProportionalMillionths: lnwire.MilliSatoshi(
				b.ProportionalFee,
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
			ToNodeFeatures: features,
		}

		hints[fromNode] = []*channeldb.CachedEdgePolicy{
			hint,
		}
	}

	return hints
}
