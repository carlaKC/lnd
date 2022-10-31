package routing

import (
	"testing"

	"github.com/go-errors/errors"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

// TestBlindedParams tests updating of pathfinding parameters to account for
// the presence of a blinded path.
func TestBlindedParams(t *testing.T) {
	var (
		amount        = lnwire.MilliSatoshi(1000)
		limit  uint32 = 150
		delta  uint16 = 40

		blindedBase  uint32 = 500
		blindedRate  uint32 = 10000
		blindedDelta uint16 = 6
	)

	tests := []struct {
		name          string
		blindedPath   *BlindedPayment
		amt           lnwire.MilliSatoshi
		cltvLimit     uint32
		cltvDelta     uint16
		expectedAmt   lnwire.MilliSatoshi
		expectedLimit uint32
		expectedDelta uint16
		err           error
	}{
		{
			name:          "no blinded path",
			blindedPath:   nil,
			amt:           amount,
			cltvLimit:     limit,
			cltvDelta:     delta,
			expectedAmt:   amount,
			expectedLimit: limit,
			expectedDelta: delta,
		},
		{
			name:        "cltv limit set with blinded path",
			cltvLimit:   10,
			blindedPath: &BlindedPayment{},
			err:         errCltvLimitSet,
		},
		{
			name:        "cltv delta set with blinded path",
			blindedPath: &BlindedPayment{},
			cltvDelta:   10,
			err:         errCltvDeltaSet,
		},
		{
			name: "blinded path",
			blindedPath: &BlindedPayment{
				AggregateRelay: &lnwire.PaymentRelay{
					CltvExpiryDelta: blindedDelta,
					BaseFee:         blindedBase,
					FeeRate:         blindedRate,
				},
			},
			amt:       amount,
			cltvDelta: delta,
			// 1000 msat * 10,000 ppm = 10 sat proportional fee
			expectedAmt:   amount + lnwire.MilliSatoshi(blindedBase+10),
			expectedDelta: blindedDelta,
		},
	}

	for _, testCase := range tests {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			amt, limit, delta, err := blindedPaymentParams(
				testCase.amt, testCase.cltvLimit,
				testCase.cltvDelta, testCase.blindedPath,
			)
			require.True(t, errors.Is(err, testCase.err))
			require.Equal(t, testCase.expectedLimit, limit)
			require.Equal(t, testCase.expectedAmt, amt)
			require.Equal(t, testCase.expectedDelta, delta)
		})
	}
}
