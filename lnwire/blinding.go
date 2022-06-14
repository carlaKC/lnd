package lnwire

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// shortChannelIDType is a record type for the outgoing channel short
	// ID.
	shortChannelIDType tlv.Type = 2

	// nextNodeType is a record type for the unblinded next node ID.
	nextNodeType tlv.Type = 4

	// paymentRelayType is the record type for a tlv containing fee and cltv
	// forwarding information.
	paymentRelayType tlv.Type = 10

	// paymentConstraintsType is a tlv containing the constraints placed
	// on a forwarded payment.
	paymentConstraintsType tlv.Type = 12
)

// BlindedRouteData contains the information that is included in a blinded route
// encrypted data blob.
type BlindedRouteData struct {
	// ShortChannelID is the channel ID of the next hop.
	ShortChannelID *ShortChannelID

	// NextNodeID is the unblinded node ID of the next hop.
	NextNodeID *btcec.PublicKey

	// RelayInfo provides the relay parameters for the hop.
	RelayInfo *PaymentRelayInfo

	// Constraints provides the payment relay constraints for the hop.
	Constraints *PaymentConstraints
}

// DecodeBlindedRouteData decodes the data provided within a blinded route.
func DecodeBlindedRouteData(r io.Reader) (*BlindedRouteData, error) {
	var (
		routeData = &BlindedRouteData{
			RelayInfo:   &PaymentRelayInfo{},
			Constraints: &PaymentConstraints{},
		}

		shortID uint64
	)

	records := []tlv.Record{
		tlv.MakePrimitiveRecord(shortChannelIDType, &shortID),
		tlv.MakePrimitiveRecord(nextNodeType, &routeData.NextNodeID),
		newPaymentRelayRecord(routeData.RelayInfo),
		newPaymentConstraintsRecord(routeData.Constraints),
	}

	stream, err := tlv.NewStream(records...)
	if err != nil {
		return nil, err
	}

	tlvMap, err := stream.DecodeWithParsedTypes(r)
	if err != nil {
		return nil, err
	}

	if _, ok := tlvMap[paymentRelayType]; !ok {
		routeData.RelayInfo = nil
	}

	if _, ok := tlvMap[paymentConstraintsType]; !ok {
		routeData.Constraints = nil
	}

	if _, ok := tlvMap[shortChannelIDType]; ok {
		shortID := NewShortChanIDFromInt(shortID)
		routeData.ShortChannelID = &shortID
	}

	return routeData, nil
}

// EncodeBlindedRouteData encodes the blinded route data provided.
func EncodeBlindedRouteData(data *BlindedRouteData) ([]byte, error) {
	var (
		w       = new(bytes.Buffer)
		records []tlv.Record
	)

	if data.ShortChannelID != nil {
		shortID := data.ShortChannelID.ToUint64()

		shortIDRecord := tlv.MakePrimitiveRecord(
			shortChannelIDType, &shortID,
		)

		records = append(records, shortIDRecord)
	}

	if data.NextNodeID != nil {
		nodeIDRecord := tlv.MakePrimitiveRecord(
			nextNodeType, &data.NextNodeID,
		)
		records = append(records, nodeIDRecord)
	}

	if data.RelayInfo != nil {
		relayRecord := newPaymentRelayRecord(data.RelayInfo)
		records = append(records, relayRecord)
	}

	if data.Constraints != nil {
		constraintsRecord := newPaymentConstraintsRecord(data.Constraints)
		records = append(records, constraintsRecord)
	}

	stream, err := tlv.NewStream(records...)
	if err != nil {
		return nil, err
	}

	if err := stream.Encode(w); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}

// PaymentRelayInfo describes the relay parameters for a hop in a blinded route.
type PaymentRelayInfo struct {
	// FeeBase is the base fee for the payment.
	FeeBase uint32

	// FeeProportinal is the proportional fee for the payment.
	FeeProportinal uint32

	// CltvDelta is the expiry delta for the hop.
	CltvDelta uint16
}

// newPaymentRelayRecord creates a tlv.Record that encodes the payment relay
// (type 10) type for an encrypted blob payload.
func newPaymentRelayRecord(info *PaymentRelayInfo) tlv.Record {
	return tlv.MakeDynamicRecord(
		paymentRelayType, &info, func() uint64 {
			// uint32 / uint32 / uint16
			return 4 + 4 + 2
		}, encodePaymentRelay, decodePaymentRelay,
	)
}

func encodePaymentRelay(w io.Writer, val interface{}, _ *[8]byte) error {
	if t, ok := val.(**PaymentRelayInfo); ok {
		// TODO(carla): use existing buffer for 8 bytes, then write
		// then use for final 2?
		var buf [10]byte

		relayInfo := *t

		binary.BigEndian.PutUint32(buf[:4], relayInfo.FeeBase)
		binary.BigEndian.PutUint32(buf[4:8], relayInfo.FeeProportinal)
		binary.BigEndian.PutUint16(buf[8:], relayInfo.CltvDelta)

		_, err := w.Write(buf[:])
		return err
	}

	return tlv.NewTypeForEncodingErr(val, "*hop.paymentRelayInfo")
}

func decodePaymentRelay(r io.Reader, val interface{}, _ *[8]byte, l uint64) error {
	if t, ok := val.(**PaymentRelayInfo); ok && l == 10 {
		var buf [10]byte

		_, err := io.ReadFull(r, buf[:])
		if err != nil {
			return err
		}

		relayInfo := *t

		relayInfo.FeeBase = binary.BigEndian.Uint32(buf[:4])
		relayInfo.FeeProportinal = binary.BigEndian.Uint32(buf[4:8])
		relayInfo.CltvDelta = binary.BigEndian.Uint16(buf[8:])

		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "*hop.paymentRelayInfo", l, 10)
}

// PaymentConstraints describes the restrictions placed on a payment.
type PaymentConstraints struct {
	// MaxCltvExpiry is the maximum cltv for the payment.
	MaxCltvExpiry uint32

	// HtlcMinimumMsat is the minimum htlc size for the payment.
	HtlcMinimumMsat MilliSatoshi

	// AllowedFeatures is the set of features allowed by the hop.
	AllowedFeatures []byte
}

func newPaymentConstraintsRecord(constraints *PaymentConstraints) tlv.Record {
	return tlv.MakeDynamicRecord(
		paymentConstraintsType, &constraints, func() uint64 {
			varBytes := tlv.SizeVarBytes(
				&constraints.AllowedFeatures,
			)

			// uint32 / uint64 / varbytes
			return 4 + 8 + varBytes()
		},
		encodePaymentConstraints, decodePaymentConstraints,
	)
}

func encodePaymentConstraints(w io.Writer, val interface{}, _ *[8]byte) error {
	if c, ok := val.(**PaymentConstraints); ok {
		// then use for final 2?
		// then use for final 2?
		// TODO(carla): as above?
		var buf [12]byte

		constraints := *c

		binary.BigEndian.PutUint32(buf[:4], constraints.MaxCltvExpiry)
		binary.BigEndian.PutUint64(
			buf[4:12], uint64(constraints.HtlcMinimumMsat),
		)

		if _, err := w.Write(buf[:]); err != nil {
			return err
		}

		_, err := w.Write(constraints.AllowedFeatures)
		return err
	}

	return tlv.NewTypeForEncodingErr(val, "*paymentConstraints")
}

func decodePaymentConstraints(r io.Reader, val interface{}, _ *[8]byte,
	l uint64) error {

	if c, ok := val.(**PaymentConstraints); ok {

		buf := make([]byte, l)

		_, err := io.ReadFull(r, buf[:])
		if err != nil {
			return err
		}

		payConstraints := *c

		payConstraints.MaxCltvExpiry = binary.BigEndian.Uint32(buf[:4])
		payConstraints.HtlcMinimumMsat = MilliSatoshi(
			binary.BigEndian.Uint64(buf[4:12]),
		)
		payConstraints.AllowedFeatures = buf[12:]

		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "*paymentConstraints", l, l)
}
