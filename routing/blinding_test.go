package routing

import (
	"testing"

	sphinx "github.com/lightningnetwork/lightning-onion"
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
			name: "no relay info",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{},
			},
			err: ErrNoRelayInfo,
		},
		{
			name: "no constraints",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{},
				RelayInfo:   &AggregateRelay{},
			},
			err: ErrNoConstraints,
		},
		{
			name: "insufficient hops",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{
					BlindedHops: []*sphinx.BlindedHopInfo{},
				},
				RelayInfo:   &AggregateRelay{},
				Constraints: &AggregateConstraints{},
			},
			err: ErrInsufficientBlindedHops,
		},
		{
			name: "valid",
			payment: &BlindedPayment{
				BlindedPath: &sphinx.BlindedPath{
					BlindedHops: []*sphinx.BlindedHopInfo{
						{},
					},
				},
				RelayInfo:   &AggregateRelay{},
				Constraints: &AggregateConstraints{},
			},
		},
	}

	for _, testCase := range tests {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.payment.Validate()
			require.ErrorIs(t, err, testCase.err)
		})
	}
}
