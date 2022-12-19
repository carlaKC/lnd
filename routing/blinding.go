package routing

import (
	"fmt"

	"github.com/go-errors/errors"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
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
