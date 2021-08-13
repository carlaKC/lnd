package lnwire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// typeErroneousField contains to a message type, field number and
	// field value that cased an error.
	typeErroneousField tlv.Type = 1

	// typeSuggestedValue contains a suggested value for a field that
	// caused an error.
	typeSuggestedValue tlv.Type = 3

	// typeErrorCode contains an error code that is not related to a
	// specific message/field combination.
	typeErrorCode tlv.Type = 5
)

var errUnknownCombination = errors.New("message and field number " +
	"combination unknown")

// errFieldHelper has the functionality we need to serialize and deserialize
// the erroneous/suggested value tlvs.
type errFieldHelper struct {
	fieldName string
	encode    func(interface{}) ([]byte, error)
	decode    func([]byte) (interface{}, error)
}

type erroneousField struct {
	messageType MessageType
	fieldNumber uint16
	value       []byte
}

// getFieldHelper looks up the helper struct for a message/ field combination
// in our map of known structured errors, returning a nil struct if the
// combination is unknown.
func getFieldHelper(errField erroneousField) *errFieldHelper {
	msgFields, ok := supportedStructuredError[errField.messageType]
	if !ok {
		return nil
	}

	fieldHelper, ok := msgFields[errField.fieldNumber]
	if !ok {
		return nil
	}

	return fieldHelper
}

// encodeErroneousField encodes the erroneous message type and field number
// in a single tlv record.
func encodeErroneousField(w io.Writer, val interface{}, buf *[8]byte) error {
	errField, ok := val.(*erroneousField)
	if !ok {
		return fmt.Errorf("expected erroneous field, got: %T",
			val)
	}

	msgNr := uint16(errField.messageType)
	if err := tlv.EUint16(w, &msgNr, buf); err != nil {
		return err
	}

	if err := tlv.EUint16(w, &errField.fieldNumber, buf); err != nil {
		return err
	}

	if err := tlv.EVarBytes(w, &errField.value, buf); err != nil {
		return err
	}

	return nil
}

// decodeErroneousField decodes an erroneous field tlv message type and field
// number. We can't use 2x tlv.DUint16 because these functions expect to only
// read 2 bytes from the reader, and we have 2x 2bytes concatenated here plus
// the var bytes value.
func decodeErroneousField(r io.Reader, val interface{}, buf *[8]byte,
	l uint64) error {

	errField, ok := val.(*erroneousField)
	if !ok {
		return fmt.Errorf("expected erroneous field, got: %T",
			val)
	}

	if l < 4 {
		return fmt.Errorf("expected at least 4 bytes for erroneous "+
			"field, got: %v", l)
	}

	n, err := r.Read(buf[:2])
	if err != nil {
		return err
	}
	if n != 2 {
		return fmt.Errorf("expected 2 bytes for message type, got: %v",
			n)
	}

	msgType := MessageType(binary.BigEndian.Uint16(buf[:2]))

	n, err = r.Read(buf[2:4])
	if err != nil {
		return err
	}
	if n != 2 {
		return fmt.Errorf("expected 2 bytes for field number, got: %v",
			n)
	}
	fieldNumber := binary.BigEndian.Uint16(buf[2:4])

	*errField = erroneousField{
		messageType: msgType,
		fieldNumber: fieldNumber,
	}

	// Now that we've read the first two elements out of the buffer, we can
	// read the var bytes value out using the standard tlv method, since
	// it's all that's left.
	if err := tlv.DVarBytes(r, &errField.value, buf, l-4); err != nil {
		return err
	}

	return nil
}

// createErrFieldRecord creates a tlv record for our erroneous field.
func createErrFieldRecord(value *erroneousField) tlv.Record {
	// Record size:
	// 2 bytes message type
	// 2 bytes field number
	// var bytes value
	sizeFunc := func() uint64 {
		return 2 + 2 + tlv.SizeVarBytes(&value.value)()
	}

	return tlv.MakeDynamicRecord(
		typeErroneousField, value, sizeFunc,
		encodeErroneousField, decodeErroneousField,
	)
}

func encode32Byte(val interface{}) ([]byte, error) {
	return nil, nil
}

func decode32Byte(val []byte) (interface{}, error) {
	return nil, nil
}

func encodeU16(val interface{}) ([]byte, error) {
	return nil, nil
}

func decodeU16(val []byte) (interface{}, error) {
	return nil, nil
}

func encodeU32(val interface{}) ([]byte, error) {
	return nil, nil
}

func decodeU32(val []byte) (interface{}, error) {
	return nil, nil
}

func encodeU64(val interface{}) ([]byte, error) {
	return nil, nil
}

func decodeU64(val []byte) (interface{}, error) {
	return nil, nil
}

// supportedStructuredError contains a map of specification message types to
// helpers for each of the fields in that message for which we understand
// structured errors. If a message is not contained in this map, we do not
// understand structured errors for that message or field.
//
// Field number is defined as follows:
// * For fixed fields: 0-based index of the field as defined in #BOLT 1
// * For TLV fields: number of fixed fields + TLV field number
var supportedStructuredError = map[MessageType]map[uint16]*errFieldHelper{
	MsgOpenChannel: map[uint16]*errFieldHelper{
		0: &errFieldHelper{
			fieldName: "chain hash",
			encode:    encode32Byte,
			decode:    decode32Byte,
		},
		1: &errFieldHelper{
			fieldName: "channel id",
			encode:    encode32Byte,
			decode:    decode32Byte,
		},
		2: &errFieldHelper{
			fieldName: "funding sats",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		3: &errFieldHelper{
			fieldName: "push amount",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		4: &errFieldHelper{
			fieldName: "dust limit",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		5: &errFieldHelper{
			fieldName: "max htlc value in flight msat",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		6: &errFieldHelper{
			fieldName: "channel reserve",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		7: &errFieldHelper{
			fieldName: "htlc minimum msat",
			encode:    encodeU64,
			decode:    decodeU64,
		},
		8: &errFieldHelper{
			fieldName: "feerate per kw",
			encode:    encodeU32,
			decode:    decodeU32,
		},
		9: &errFieldHelper{
			fieldName: "to self delay",
			encode:    encodeU16,
			decode:    decodeU16,
		},
		10: &errFieldHelper{
			fieldName: "max accepted htlcs",
			encode:    encodeU16,
			decode:    decodeU16,
		},
	},
	MsgAcceptChannel: map[uint16]*errFieldHelper{
		5: &errFieldHelper{
			fieldName: "min depth",
			encode:    encodeU32,
			decode:    decodeU32,
		},
	},
}

// Compile time assertion that StructuredError implements the error interface.
var _ error = (*StructuredError)(nil)

// StrucutredError contains structured error information for an error.
type StructuredError struct {
	erroneousField
	suggestedValue []byte
}

// ErroneousValue returns the erroneous value for an error. If the value is not
// set or the message type/ field number combination are unknown, a nil value
// will be returned.
func (s *StructuredError) ErroneousValue() (interface{}, error) {
	if s.value == nil {
		return nil, nil
	}

	fieldHelper := getFieldHelper(s.erroneousField)
	if fieldHelper == nil {
		return nil, nil
	}

	return fieldHelper.decode(s.value)
}

// SuggestedValue returns the suggested value for an error. If the value is not
// set or the message type/ field number combination are unknown, a nil value
// will be returned.
func (s *StructuredError) SuggestedValue() (interface{}, error) {
	if s.suggestedValue == nil {
		return nil, nil
	}

	fieldHelper := getFieldHelper(s.erroneousField)
	if fieldHelper == nil {
		return nil, nil
	}

	return fieldHelper.decode(s.suggestedValue)
}

// Error returns an error string for our structured errors, including the
// suggested value if it is present.
func (s *StructuredError) Error() string {
	errStr := fmt.Sprintf("Message: %v failed", s.messageType)

	// Include field name in our error string if we know it.
	helper := getFieldHelper(s.erroneousField)
	if helper == nil {
		return fmt.Sprintf("%v, field: %v", errStr, s.fieldNumber)
	}

	errStr = fmt.Sprintf("%v, field: %v (%v)", errStr, helper.fieldName,
		s.fieldNumber)

	if s.value != nil {
		errStr = fmt.Sprintf("%v, erroneous value: %v", errStr,
			s.value)
	}

	if s.suggestedValue != nil {
		errStr = fmt.Sprintf("%v, suggested value: %v", errStr,
			s.suggestedValue)
	}

	return errStr
}

// NewStructuredError creates a structured error containing information about
// the field we have a problem with.
func NewStructuredError(messageType MessageType, fieldNumber uint16,
	erroneousValue, suggestedValue interface{}) *StructuredError {

	// Panic on creation of unsupported errors because we expect them
	// to be added to our list of supported errors.
	errField := erroneousField{
		messageType: messageType,
		fieldNumber: fieldNumber,
	}

	fieldHelper := getFieldHelper(errField)
	if fieldHelper == nil {
		panic(fmt.Sprintf("Structured errors not supported for: %v "+
			"field: %v", messageType, fieldNumber))
	}

	structuredErr := &StructuredError{
		erroneousField: errField,
	}

	// Encode straight to bytes so that the tlv record can just encode/
	// decode var bytes rather than needing to know message type + field
	// in advance to parse the record.
	//
	// TODO(carla): how to handle this error?
	if erroneousValue != nil {
		erroneous, err := fieldHelper.encode(erroneousValue)
		if err != nil {
			panic(fmt.Sprintf("erroneous value encode failed: %v",
				err))
		}

		structuredErr.value = erroneous
	}

	if suggestedValue != nil {
		suggested, err := fieldHelper.encode(suggestedValue)
		if err != nil {
			panic(fmt.Sprintf("suggested value encode failed: %v",
				err))
		}

		structuredErr.suggestedValue = suggested
	}

	return structuredErr
}

// ToWireError creates an error containing TLV fields that are used to point
// the recipient towards problematic field values.
func (s *StructuredError) ToWireError(chanID ChannelID) (*Error, error) {
	// Lookup a helper for the message + field that we intend on adding
	// to our error. We expect these entries to be present, as this is
	// enforced in the constructor.
	fieldHelper := getFieldHelper(s.erroneousField)
	if fieldHelper == nil {
		return nil, fmt.Errorf("%w (%v/%v)", errUnknownCombination,
			s.messageType, s.fieldNumber)
	}

	return s.packRecords(chanID, fieldHelper)
}

// packRecords returns a wire error with all our structured error fields packed
// into the ExtraData field. This is separated into its own method so that we
// can easily pack errors with unknown message/field combinations for testing.
func (s *StructuredError) packRecords(chanID ChannelID,
	fieldHelper *errFieldHelper) (*Error, error) {

	resp := &Error{
		ChanID: chanID,
		Data:   ErrorData(s.Error()),
	}

	records := []tlv.Record{
		createErrFieldRecord(&s.erroneousField),
	}

	if s.suggestedValue != nil {
		record := tlv.MakePrimitiveRecord(
			typeSuggestedValue, &s.suggestedValue,
		)

		records = append(records, record)
	}

	if err := resp.ExtraData.PackRecords(records...); err != nil {
		return nil, err
	}

	return resp, nil
}

// CodedError is a structured error that relies on an error code to provide
// additional information about an error.
type CodedError uint8

// Compile time check that CodedError implements error.
var _ error = (*CodedError)(nil)

// Error returns an error string for a coded error.
func (c CodedError) Error() string {
	return fmt.Sprintf("Coded error: %d", c)
}

// ToWireError returns a wire error with our error code packed into the
// ExtraData field.
func (c CodedError) ToWireError(chanID ChannelID) (*Error, error) {
	resp := &Error{
		ChanID: chanID,
		Data:   ErrorData(c.Error()),
	}

	errCode := uint8(c)
	records := []tlv.Record{
		tlv.MakePrimitiveRecord(typeErrorCode, &errCode),
	}

	if err := resp.ExtraData.PackRecords(records...); err != nil {
		return nil, err
	}

	return resp, nil

}

// StructuredErrorFromWire extracts a structured error from our error's extra
// data, if present.
func StructuredErrorFromWire(err *Error) (error, error) {
	if err == nil {
		return nil, nil
	}

	if len(err.ExtraData) == 0 {
		return nil, nil
	}

	// First we try to extract our message and field number records.
	var (
		structuredErr = &StructuredError{}
		codedErr      uint8
	)

	records := []tlv.Record{
		createErrFieldRecord(&structuredErr.erroneousField),
		tlv.MakePrimitiveRecord(
			typeSuggestedValue, &structuredErr.suggestedValue,
		),
		tlv.MakePrimitiveRecord(
			typeErrorCode, &codedErr,
		),
	}

	tlvs, extractErr := err.ExtraData.ExtractRecords(records...)
	if extractErr != nil {
		return nil, extractErr
	}

	// If we have the error code TLV, we don't expect any other fields so
	// we just return a coded error using the value.
	if _, ok := tlvs[typeErrorCode]; ok {
		return CodedError(codedErr), nil
	}

	// If we don't know the problematic message type and field, we can't
	// add any additional information to this error.
	if _, ok := tlvs[typeErroneousField]; !ok {
		return nil, nil
	}

	return structuredErr, nil
}
