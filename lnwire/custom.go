package lnwire

import (
	"bytes"
	"fmt"
	"io"
)

var (
	// CustomTypeStart is the start of the custom type range for peer
	// messages as defined in BOLT 01.
	CustomTypeStart MessageType = 32768

	// CustomTypeOverride contains a set of message types < CustomTypeStart
	// that lnd allows to be treated as custom messages. This allows us to
	// override messages reserved for the protocol level and treat them as
	// custom messages.
	CustomTypeOverride []MessageType
)

// SetCustomOverrides validates that the set of override types are outside of
// the custom message range (there's no reason to override messages that are
// already within the range), and updates the CustomTypeOverride global to hold
// this set of message types.
func SetCustomOverrides(overrideTypes []uint64) error {
	CustomTypeOverride = make([]MessageType, len(overrideTypes))

	for i, t := range overrideTypes {
		msgType := MessageType(t)

		if msgType >= CustomTypeStart {
			return fmt.Errorf("can't override type: %v, already "+
				"in custom range", t)
		}

		CustomTypeOverride[i] = msgType
	}

	return nil
}

// IsCustomOverride returns a bool indicating whether the message type is one
// of the protocol messages that we override for custom use.
func IsCustomOverride(t MessageType) bool {
	for _, override := range CustomTypeOverride {
		if t == override {
			return true
		}
	}

	return false
}

// Custom represents an application-defined wire message.
type Custom struct {
	Type MessageType
	Data []byte
}

// A compile time check to ensure FundingCreated implements the lnwire.Message
// interface.
var _ Message = (*Custom)(nil)

// NewCustom instantiates a new custom message.
func NewCustom(msgType MessageType, data []byte) (*Custom, error) {
	if msgType < CustomTypeStart && !IsCustomOverride(msgType) {
		return nil, fmt.Errorf("msg type: %v not in custom range: %v "+
			"and not overridden: %v", msgType, CustomTypeStart,
			CustomTypeOverride)
	}

	return &Custom{
		Type: msgType,
		Data: data,
	}, nil
}

// Encode serializes the target Custom message into the passed io.Writer
// implementation.
//
// This is part of the lnwire.Message interface.
func (c *Custom) Encode(b *bytes.Buffer, pver uint32) error {
	_, err := b.Write(c.Data)
	return err
}

// Decode deserializes the serialized Custom message stored in the passed
// io.Reader into the target Custom message.
//
// This is part of the lnwire.Message interface.
func (c *Custom) Decode(r io.Reader, pver uint32) error {
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		return err
	}

	c.Data = b.Bytes()

	return nil
}

// MsgType returns the uint32 code which uniquely identifies this message as a
// Custom message on the wire.
//
// This is part of the lnwire.Message interface.
func (c *Custom) MsgType() MessageType {
	return c.Type
}
