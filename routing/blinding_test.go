package routing

import (
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

// TestBlindedPathValidation tests validation of blinded paths.
func TestBlindedPathValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payment *BlindedPayment
		err     error
	}{
		{
			name:    "no path",
			payment: &BlindedPayment{},
			err:     ErrNoBlindedPath,
		},
		{
			name: "no info",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{},
			},
			err: ErrNoPaymentInfo,
		},
		{
			name: "insufficient hops",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{
					BlindedHops: []*sphinx.BlindedHopInfo{
						{},
					},
				},
				PaymentInfo: &lnwire.PaymentInfo{},
			},
			err: ErrInsufficientBlindedHops,
		},
		{
			name: "valid",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{
					BlindedHops: []*sphinx.BlindedHopInfo{
						{}, {},
					},
				},
				PaymentInfo: &lnwire.PaymentInfo{},
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.payment.Validate()
			require.True(t, errors.Is(err, testCase.err))
		})
	}
}

// TestBlindedPaymentToHints tests conversion of a blinded path to a chain of
// route hints. As our function assumes that the blinded payment has already
// been validated (ie, has at least 2 hops), we don't test cases with an invalid
// number of hops.
func TestBlindedPaymentToHints(t *testing.T) {
	t.Parallel()

	var (
		_, pk1  = btcec.PrivKeyFromBytes([]byte{1})
		_, pkb1 = btcec.PrivKeyFromBytes([]byte{2})
		_, pkb2 = btcec.PrivKeyFromBytes([]byte{3})
		_, pkb3 = btcec.PrivKeyFromBytes([]byte{4})

		v1  = route.NewVertex(pk1)
		vb2 = route.NewVertex(pkb2)
		vb3 = route.NewVertex(pkb3)

		baseFee   uint32 = 1000
		ppmFee    uint32 = 500
		cltvDelta uint16 = 140
		htlcMin          = lnwire.MilliSatoshi(100)
		htlcMax          = lnwire.MilliSatoshi(1000)

		rawFeatures = lnwire.NewRawFeatureVector(
			lnwire.AMPOptional,
		)

		features = lnwire.NewFeatureVector(
			rawFeatures, lnwire.Features,
		)
	)

	blindedPayment := &BlindedPayment{
		BlindedPath: &sphinx.BlindedPath{
			IntroductionPoint: pk1,
			BlindedHops: []*sphinx.BlindedHopInfo{
				{
					NodePub: pkb1,
				},
				{
					NodePub: pkb2,
				},
				{
					NodePub: pkb3,
				},
			},
		},
		PaymentInfo: &lnwire.PaymentInfo{
			BaseFee:         baseFee,
			FeeRate:         ppmFee,
			CltvExpiryDelta: cltvDelta,
			HtlcMinimumMsat: htlcMin,
			HtlcMaximumMsat: htlcMax,
			Features:        features,
		},
	}

	expected := map[route.Vertex][]*channeldb.CachedEdgePolicy{
		v1: {
			{
				TimeLockDelta: cltvDelta,
				MinHTLC:       htlcMin,
				MaxHTLC:       htlcMax,
				FeeBaseMSat:   lnwire.MilliSatoshi(baseFee),
				FeeProportionalMillionths: lnwire.MilliSatoshi(
					ppmFee,
				),
				ToNodePubKey: func() route.Vertex {
					return vb2
				},
				ToNodeFeatures: features,
			},
		},
		vb2: {
			{
				ToNodePubKey: func() route.Vertex {
					return vb3
				},
			},
		},
	}
	actual := blindedPayment.toRouteHints()

	require.Equal(t, len(expected), len(actual))
	for vertex, expectedHint := range expected {
		actualHint, ok := actual[vertex]
		require.True(t, ok, "node not found: %v", vertex)

		require.Len(t, expectedHint, 1)
		require.Len(t, actualHint, 1)

		// We can't assert that our functions are equal, so we check
		// their output and then mark as nil so that we can use
		// require.Equal for all our other fields.
		require.Equal(t, expectedHint[0].ToNodePubKey(),
			actualHint[0].ToNodePubKey())

		actualHint[0].ToNodePubKey = nil
		expectedHint[0].ToNodePubKey = nil

		require.Equal(t, expectedHint[0], actualHint[0])
	}
}
