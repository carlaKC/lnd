package routing

import (
	"errors"
	"testing"

	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
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
