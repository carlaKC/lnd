package record

import "github.com/lightningnetwork/lnd/lnwire"

// PaymentRelayInfo describes the relay policy for a blinded path.
type PaymentRelayInfo struct {
	// CltvExpiryDelta is the expiry delta for the payment.
	CltvExpiryDelta uint16

	// FeeRate is the fee rate that will be charged per millionth of a
	// satoshi.
	FeeRate uint32

	// BaseFee is the per-htlc fee charged.
	BaseFee uint32
}

// PaymentConstraints is a set of restrictions on a payment.
type PaymentConstraints struct {
	// MaxCltvExpiry is the maximum expiry height for the payment.
	MaxCltvExpiry uint32

	// HtlcMinimumMsat is the minimum htlc size for the payment.
	HtlcMinimumMsat lnwire.MilliSatoshi
}
