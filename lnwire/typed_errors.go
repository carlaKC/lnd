package lnwire

import (
	"fmt"
	"strings"

	"github.com/lightningnetwork/lnd/tlv"
)

const (
	typeMessageType    tlv.Type = 1
	typeFieldNum       tlv.Type = 3
	typeSuggestedValue tlv.Type = 5
	typeErroneousValue tlv.Type = 7
	typeErrorCode      tlv.Type = 9
)

// ErrorCode is an enum that represents errors that cannot be represented using
// the erroneous message/field structure.
type ErrorCode uint8

// structuredErrorHelper has the functionality we need to create structured
// errors.
type structuredErrorHelper struct {
	fieldName       string
	makeValueRecord func(interface{}, tlv.Type) tlv.Record
}

func make32ByteRecord(val interface{}, tlvType tlv.Type) tlv.Record {
	byteSliceVal := val.([32]byte)
	return tlv.MakePrimitiveRecord(tlvType, &byteSliceVal)
}

func makeUint64Record(val interface{}, tlvType tlv.Type) tlv.Record {
	int64Val := val.(uint64)
	return tlv.MakePrimitiveRecord(tlvType, &int64Val)
}

func makeUint32Record(val interface{}, tlvType tlv.Type) tlv.Record {
	uint32Val := val.(uint32)
	return tlv.MakePrimitiveRecord(tlvType, &uint32Val)
}

func makeUint16Record(val interface{}, tlvType tlv.Type) tlv.Record {
	uint32Val := val.(uint32)
	return tlv.MakePrimitiveRecord(tlvType, &uint32Val)
}

// supportedStructuredError contains a map of specification message types to
// helpers for each of the fields in that message for which we understand
// structured errors. If a message is not contained in this map, we do not
// understand structured errors for that message or field.
//
// Field number is defined as follows:
// * For fixed fields: 0-based index of the field as defined in #BOLT 1
// * For TLV fields: number of fixed fields + TLV field number
var supportedStructuredError = map[MessageType]map[uint16]structuredErrorHelper{
	MsgOpenChannel: map[uint16]structuredErrorHelper{
		0: structuredErrorHelper{
			fieldName: "chain hash",
			// todo - use here.
			makeValueRecord: make32ByteRecord,
		},
		1: structuredErrorHelper{
			fieldName:       "channel id",
			makeValueRecord: make32ByteRecord,
		},
		2: structuredErrorHelper{
			fieldName:       "funding sats",
			makeValueRecord: makeUint64Record,
		},
		3: structuredErrorHelper{
			fieldName:       "push amount",
			makeValueRecord: makeUint64Record,
		},
		4: structuredErrorHelper{
			fieldName:       "dust limit sat",
			makeValueRecord: makeUint64Record,
		},
		5: structuredErrorHelper{
			fieldName:       "max htlc value in flight msat",
			makeValueRecord: makeUint64Record,
		},
		6: structuredErrorHelper{
			fieldName:       "channel reserve",
			makeValueRecord: makeUint64Record,
		},
		7: structuredErrorHelper{
			fieldName:       "htlc minimum msat",
			makeValueRecord: makeUint64Record,
		},
		8: structuredErrorHelper{
			fieldName:       "feerate per kw",
			makeValueRecord: makeUint32Record,
		},
		9: structuredErrorHelper{
			fieldName:       "to self delay",
			makeValueRecord: makeUint16Record,
		},
		10: structuredErrorHelper{
			fieldName:       "max accepted htlcs",
			makeValueRecord: makeUint16Record,
		},
	},
	MsgAcceptChannel: map[uint16]structuredErrorHelper{
		5: structuredErrorHelper{
			fieldName:       "min depth",
			makeValueRecord: makeUint32Record,
		},
	},
}

// StrucutredError contains structured error information for an error.
type StructuredError struct {
	messageType    *MessageType
	fieldNumber    *uint16
	suggestedValue interface{}
	erroneousValue interface{}
	errorCode      *ErrorCode
}

// TODO(carla): find a cleaner way to expose these.
func (s *StructuredError) MessageType() (MessageType, bool) {
	if s.messageType == nil {
		return 0, false
	}

	return *s.messageType, true
}

func (s *StructuredError) FieldNumber() (uint16, bool) {
	if s.fieldNumber == nil {
		return 0, false
	}

	return *s.fieldNumber, true
}

func (s *StructuredError) SuggestedValue() interface{} {
	return s.suggestedValue
}

func (s *StructuredError) ErroneousValue() interface{} {
	return s.erroneousValue
}

// Error returns an error string for our structured errors, including the
// suggested value if it is present.
func (s *StructuredError) Error() string {
	errStrs := []string{
		"Structured error",
	}

	if s.messageType != nil {
		errStrs = append(
			errStrs, fmt.Sprintf("message: %v", *s.messageType),
		)
	}

	if s.fieldNumber != nil {
		errStrs = append(
			errStrs, fmt.Sprintf("field: %v", *s.fieldNumber),
		)
	}

	if s.erroneousValue != nil {
		errStrs = append(errStrs, fmt.Sprintf("rejected value: %v",
			s.erroneousValue))
	}

	if s.suggestedValue != nil {
		errStrs = append(errStrs, fmt.Sprintf("suggested value: %v",
			s.suggestedValue))
	}

	if s.errorCode != nil {
		errStrs = append(errStrs, fmt.Sprintf("error code: %v",
			*s.errorCode))
	}

	return strings.Join(errStrs, ", ")
}

// NewStructuredError creates a structured error containing information about
// the field we have a problem with.
func NewStructuredError(messageType MessageType, fieldNumber uint16,
	suggestedValue, erroneousValue interface{}) *StructuredError {

	// Panic on creation of unsupported errors because we expect them
	// to be added to our list of supported errors.
	supportedFields, ok := supportedStructuredError[messageType]
	if !ok {
		panic(fmt.Sprintf("Structured errors not supported for: %v",
			messageType))
	}

	_, ok = supportedFields[fieldNumber]
	if !ok {
		panic(fmt.Sprintf("Structured errors not supported for: %v "+
			"field: %v", messageType, fieldNumber))
	}

	return &StructuredError{
		messageType:    &messageType,
		fieldNumber:    &fieldNumber,
		suggestedValue: suggestedValue,
		erroneousValue: erroneousValue,
	}
}

// NewCodedError creates a structured error containing an error code.
func NewCodedError(messageType MessageType,
	errorCode ErrorCode) *StructuredError {

	return &StructuredError{
		errorCode:   &errorCode,
		messageType: &messageType,
	}
}

// ToWireError creates an error containing TLV fields that are used to point
// the recipient towards problematic field values.
func (s *StructuredError) ToWireError(chanID ChannelID) *Error {
	resp := &Error{
		ChanID: chanID,
		Data:   ErrorData(s.Error()),
	}

	var records []tlv.Record

	if s.errorCode != nil {
		errCode := *s.errorCode
		record := tlv.MakePrimitiveRecord(typeErrorCode, &errCode)
		records = append(records, record)
	}

	if s.messageType != nil {
		msgType := uint16(*s.messageType)
		record := tlv.MakePrimitiveRecord(typeMessageType, &msgType)
		records = append(records, record)
	}

	if s.fieldNumber != nil {
		fieldNr := uint16(*s.fieldNumber)
		record := tlv.MakePrimitiveRecord(typeFieldNum, &fieldNr)
		records = append(records, record)
	}

	// We need both our field number and message type to be able to pack
	// our additional records. If they are not set, we just return pack the
	// records we have, if any, and return.
	if s.fieldNumber == nil || s.messageType == nil {
		_ = resp.ExtraData.PackRecords(records...)
		return resp
	}

	// Lookup a helper for the message + field that we intend on adding
	// to our error. We expect these entries to be present, as this is
	// enforced in the constructor.
	supportedFields := supportedStructuredError[*s.messageType]
	fieldHelper := supportedFields[*s.fieldNumber]

	if s.suggestedValue != nil {
		record := fieldHelper.makeValueRecord(
			s.suggestedValue, typeSuggestedValue,
		)
		records = append(records, record)
	}

	if s.erroneousValue != nil {
		record := fieldHelper.makeValueRecord(
			s.suggestedValue, typeSuggestedValue,
		)
		records = append(records, record)
	}

	// TODO(carla): surface this err? Panic?
	_ = resp.ExtraData.PackRecords(records...)

	return resp
}

// StructuredErrorFromWire extracts a structured error from our error's extra
// data, if present.
func StructuredErrorFromWire(err *Error) (*StructuredError, error) {
	if err == nil {
		return nil, nil
	}

	if len(err.ExtraData) == 0 {
		return nil, nil
	}

	// First we try to extract our message and field number records and
	// an error code if it is present.
	var (
		messageType, fieldNr uint16
		errorCode            uint8
	)
	records := []tlv.Record{
		tlv.MakePrimitiveRecord(typeMessageType, &messageType),
		tlv.MakePrimitiveRecord(typeFieldNum, &fieldNr),
		tlv.MakePrimitiveRecord(typeErrorCode, &errorCode),
	}

	tlvs, extractErr := err.ExtraData.ExtractRecords(records...)
	if extractErr != nil {
		return nil, extractErr
	}

	var (
		_, errCodeSet = tlvs[typeErrorCode]
		_, msgTypeSet = tlvs[typeMessageType]

		errCode = ErrorCode(errorCode)
		msgType = MessageType(messageType)

		structuredErr = &StructuredError{}
	)

	switch {
	// If an error code and message type are set, we can continue to
	// parse more fields.
	case errCodeSet && msgTypeSet:
		structuredErr.errorCode = &errCode
		structuredErr.messageType = &msgType

	// If only error code is set, we just return a structured error with
	// the error code provided, we don't have a message type so we can't
	// get more information.
	case errCodeSet:
		return &StructuredError{
			errorCode: &errCode,
		}, nil

	// If only message type is set, we can continue to parse field info.
	case msgTypeSet:
		structuredErr.messageType = &msgType

	// If neither are set, we can't get any information from this set of
	// tlvs.
	default:
		return nil, nil
	}

	// If a field number was not specified, there is no further information
	// we can get from the tlvs.
	if _, ok := tlvs[typeFieldNum]; !ok {
		return structuredErr, nil
	}

	structuredErr.fieldNumber = &fieldNr

	// If we don't have decode support for this message type, we have all
	// we need.
	supportedFields, typeSupported := supportedStructuredError[msgType]
	if !typeSupported {
		return structuredErr, nil
	}

	// If we don't have decode support for this field number, we have all
	// the information we can.
	fieldHelper, fieldSupported := supportedFields[fieldNr]
	if !fieldSupported {
		return structuredErr, nil
	}

	// Now that we know how we'll decode suggested and erroneous value tlvs
	// we create records and extract them.
	// TODO[carla]: will interfaces be ok here? does this set value.
	records = []tlv.Record{
		fieldHelper.makeValueRecord(
			structuredErr.suggestedValue, typeSuggestedValue,
		),
		fieldHelper.makeValueRecord(
			structuredErr.erroneousValue, typeErroneousValue,
		),
	}

	_, extractErr = err.ExtraData.ExtractRecords(records...)
	if extractErr != nil {
		return nil, err
	}

	return nil, nil

}
