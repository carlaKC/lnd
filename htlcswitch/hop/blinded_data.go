package hop

import (
	"encoding/binary"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightningnetwork/lnd/lnwire"
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

type blindedRouteData struct {
	shortChannelID *lnwire.ShortChannelID
	nextNodeID     *btcec.PublicKey
	relayInfo      *paymentRelayInfo
	constraints    *paymentConstraints
}

func decodeBlindedRouteData(r io.Reader) (*blindedRouteData, error) {
	var (
		routeData = &blindedRouteData{
			relayInfo:   &paymentRelayInfo{},
			constraints: &paymentConstraints{},
		}

		shortID uint64
	)

	records := []tlv.Record{
		tlv.MakePrimitiveRecord(shortChannelIDType, &shortID),
		tlv.MakePrimitiveRecord(nextNodeType, &routeData.nextNodeID),
		newPaymentRelayRecord(routeData.relayInfo),
		newPaymentConstraintsRecord(routeData.constraints),
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
		routeData.relayInfo = nil
	}

	if _, ok := tlvMap[paymentConstraintsType]; !ok {
		routeData.constraints = nil
	}

	if _, ok := tlvMap[shortChannelIDType]; ok {
		shortID := lnwire.NewShortChanIDFromInt(shortID)
		routeData.shortChannelID = &shortID
	}

	return routeData, nil
}

func encodeBlindedRouteData(w io.Writer, data *blindedRouteData) error {
	var records []tlv.Record

	if data.shortChannelID != nil {
		shortID := data.shortChannelID.ToUint64()

		shortIDRecord := tlv.MakePrimitiveRecord(
			shortChannelIDType, &shortID,
		)

		records = append(records, shortIDRecord)
	}

	if data.nextNodeID != nil {
		nodeIDRecord := tlv.MakePrimitiveRecord(
			nextNodeType, &data.nextNodeID,
		)
		records = append(records, nodeIDRecord)
	}

	if data.relayInfo != nil {
		relayRecord := newPaymentRelayRecord(data.relayInfo)
		records = append(records, relayRecord)
	}

	if data.constraints != nil {
		constraintsRecord := newPaymentConstraintsRecord(data.constraints)
		records = append(records, constraintsRecord)
	}

	stream, err := tlv.NewStream(records...)
	if err != nil {
		return err
	}

	return stream.Encode(w)
}

type paymentRelayInfo struct {
	feeBase         uint32
	feeProportional uint32
	cltvDelta       uint16
}

// newPaymentRelayRecord creates a tlv.Record that encodes the payment relay
// (type 10) type for an encrypted blob payload.
func newPaymentRelayRecord(info *paymentRelayInfo) tlv.Record {
	return tlv.MakeDynamicRecord(
		paymentRelayType, &info, func() uint64 {
			// uint32 / uint32 / uint16
			return 4 + 4 + 2
		}, encodePaymentRelay, decodePaymentRelay,
	)
}

func encodePaymentRelay(w io.Writer, val interface{}, _ *[8]byte) error {
	if t, ok := val.(**paymentRelayInfo); ok {
		// TODO(carla): use existing buffer for 8 bytes, then write
		// then use for final 2?
		var buf [10]byte

		relayInfo := *t

		binary.BigEndian.PutUint32(buf[:4], relayInfo.feeBase)
		binary.BigEndian.PutUint32(buf[4:8], relayInfo.feeProportional)
		binary.BigEndian.PutUint16(buf[8:], relayInfo.cltvDelta)

		_, err := w.Write(buf[:])
		return err
	}

	return tlv.NewTypeForEncodingErr(val, "*hop.paymentRelayInfo")
}

func decodePaymentRelay(r io.Reader, val interface{}, _ *[8]byte, l uint64) error {
	if t, ok := val.(**paymentRelayInfo); ok && l == 10 {
		var buf [10]byte

		_, err := io.ReadFull(r, buf[:])
		if err != nil {
			return err
		}

		relayInfo := *t

		relayInfo.feeBase = binary.BigEndian.Uint32(buf[:4])
		relayInfo.feeProportional = binary.BigEndian.Uint32(buf[4:8])
		relayInfo.cltvDelta = binary.BigEndian.Uint16(buf[8:])

		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "*hop.paymentRelayInfo", l, 10)
}

type paymentConstraints struct {
	maxCltv         uint32
	htlcMinimum     uint64
	allowedFeatures []byte
}

func newPaymentConstraintsRecord(constraints *paymentConstraints) tlv.Record {
	return tlv.MakeDynamicRecord(
		paymentConstraintsType, &constraints, func() uint64 {
			varBytes := tlv.SizeVarBytes(
				&constraints.allowedFeatures,
			)

			// uint32 / uint64 / varbytes
			return 4 + 8 + varBytes()
		},
		encodePaymentConstraints, decodePaymentConstraints,
	)
}

func encodePaymentConstraints(w io.Writer, val interface{}, _ *[8]byte) error {
	if c, ok := val.(**paymentConstraints); ok {
		// then use for final 2?
		// then use for final 2?
		// TODO(carla): as above?
		var buf [12]byte

		constraints := *c

		binary.BigEndian.PutUint32(buf[:4], constraints.maxCltv)
		binary.BigEndian.PutUint64(buf[4:12], constraints.htlcMinimum)

		if _, err := w.Write(buf[:]); err != nil {
			return err
		}

		_, err := w.Write(constraints.allowedFeatures)
		return err
	}

	return tlv.NewTypeForEncodingErr(val, "*paymentConstraints")
}

func decodePaymentConstraints(r io.Reader, val interface{}, _ *[8]byte,
	l uint64) error {

	if c, ok := val.(**paymentConstraints); ok {

		buf := make([]byte, l)

		_, err := io.ReadFull(r, buf[:])
		if err != nil {
			return err
		}

		payConstraints := *c

		payConstraints.maxCltv = binary.BigEndian.Uint32(buf[:4])
		payConstraints.htlcMinimum = binary.BigEndian.Uint64(buf[4:12])
		payConstraints.allowedFeatures = buf[12:]

		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "*paymentConstraints", l, l)
}
