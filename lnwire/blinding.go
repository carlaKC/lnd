package lnwire

// PaymentInfo describes the relay policy and restrictions for a blinded path.
type PaymentInfo struct {
	// BaseFee is the per-htlc fee charged.
	BaseFee uint32

	// FeeRate is the fee rate that will be charged per millionth of a
	// satoshi.
	FeeRate uint32

	// CltvExpiryDelta is the expiry delta for the payment.
	CltvExpiryDelta uint16

	// HtlcMinimumMsat is the minimum htlc size for the payment.
	HtlcMinimumMsat MilliSatoshi

	// HtlcMaximumMsat is the maximum htlc size for the payment.
	HtlcMaximumMsat MilliSatoshi

	// Features is the set of features required for the payment.
	Features *FeatureVector
}
