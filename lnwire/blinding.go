package lnwire

// PaymentRelay describes the relay policy for a blinded hop.
type PaymentRelay struct {
	// CltvExpiryDelta is the expiry delta for the payment hop.
	CltvExpiryDelta uint16

	// BaseFee is the per-htlc fee charged.
	BaseFee uint32

	// FeeRate is the fee rate that will be charged per millionth of a
	// satoshi.
	FeeRate uint32
}

// PaymentConstraints describes the restrictions placed on a payment.
type PaymentConstraints struct {
	// MaxCltvExpiry is the maximum cltv for the payment.
	MaxCltvExpiry uint32

	// HtlcMinimumMsat is the minimum htlc size for the payment.
	HtlcMinimumMsat MilliSatoshi
}
