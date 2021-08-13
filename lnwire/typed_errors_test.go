package lnwire

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStructuredErrorSerialization tests encoding and decoding structured
// errors with various combinations of tlv values present.
func TestStructuredErrorSerialization(t *testing.T) {
	// Update our global map for testing purposes.
	var knownField uint16 = 2
	uint32Helper := &errFieldHelper{
		fieldName: "uint32",
		// TODO[carla]: clean these up, ideally we don't
		// have duplication with tlv package.
		encode: func(val interface{}) ([]byte, error) {
			uint32Val, ok := val.(uint32)
			if !ok {
				return nil, fmt.Errorf("Expected "+
					"uint32, got: %T", val)
			}

			var scratch [4]byte
			binary.BigEndian.PutUint32(
				scratch[:], uint32Val,
			)

			return scratch[:], nil
		},
		decode: func(val []byte) (interface{}, error) {
			return binary.BigEndian.Uint32(val), nil
		},
	}

	supportedStructuredError = map[MessageType]map[uint16]*errFieldHelper{
		MsgOpenChannel: {
			knownField: uint32Helper,
		},
	}

	var (
		chanID         = [32]byte{1}
		errValue       = uint32(100)
		suggestedValue = uint32(101)

		allFieldsKnown = NewStructuredError(
			MsgOpenChannel, knownField, errValue, suggestedValue,
		)
	)

	// Start by encoding an error that we know all the fields for.
	encoded, err := allFieldsKnown.ToWireError(chanID)
	require.Nil(t, err)

	// Retrieve a structured error from the encoded error and assert equal.
	decoded, err := StructuredErrorFromWire(encoded)
	require.Nil(t, err)
	require.Equal(t, allFieldsKnown, decoded)

	// Access the fields and assert that we get our uint32 values again.
	decodedErrVal, err := decoded.ErroneousValue()
	require.NoError(t, err)
	require.Equal(t, errValue, decodedErrVal)

	decodedSuggestedVal, err := decoded.SuggestedValue()
	require.NoError(t, err)
	require.Equal(t, suggestedValue, decodedSuggestedVal)

	// Now we create an error that we don't know the message type for.
	// Pack records manually because we're testing a case where our own
	// packing would fail because we don't know the message type.
	unknownMessage := &StructuredError{
		erroneousField: erroneousField{
			messageType: 9,
			fieldNumber: 1,
			value:       []byte{1},
		},
		suggestedValue: []byte{2},
	}

	encoded, err = unknownMessage.packRecords(chanID, uint32Helper)
	require.NoError(t, err)

	decoded, err = StructuredErrorFromWire(encoded)
	require.NoError(t, err)
	require.Equal(t, unknownMessage, decoded)

	// Access the value fields and assert that we get nil value because we
	// don't know this message type.
	decodedErrVal, err = decoded.ErroneousValue()
	require.NoError(t, err)
	require.Nil(t, decodedErrVal)

	decodedSuggestedVal, err = decoded.SuggestedValue()
	require.NoError(t, err)
	require.Nil(t, decodedSuggestedVal)
}
