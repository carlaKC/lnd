package routing

import (
	"errors"
	"fmt"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing/route"
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
		"at least one hop")
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

// toRouteHints produces a set of chained route hints that represent a blinded
// path.
func (b *BlindedPayment) toRouteHints() map[route.Vertex][]*channeldb.CachedEdgePolicy { //nolint:lll
	// If we just have a single hop in our blinded route, it just contains
	// an introduction node (this is valid per the specification). Since
	// we have the un-blinded node ID for the introduction node, we don't
	// need to add any route hints.
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
			TimeLockDelta: b.RelayInfo.CltvExpiryDelta,
			MinHTLC:       b.Constraints.HtlcMinimumMsat,
			FeeBaseMSat: lnwire.MilliSatoshi(
				b.RelayInfo.BaseFee,
			),
			FeeProportionalMillionths: lnwire.MilliSatoshi(
				b.RelayInfo.FeeRate,
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

	log.Infof("CKC intro hint: %v -> %v (fee: %v / %v, cltv %v",
		fromNode, route.NewVertex(b.BlindedPath.BlindedHops[1].NodePub),
		b.RelayInfo.BaseFee, b.RelayInfo.FeeRate, b.RelayInfo.CltvExpiryDelta)

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

		log.Infof("CKC blinded hint: %v -> %v", fromNode, nextNode)
	}

	return hints
}
