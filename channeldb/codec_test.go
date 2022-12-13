package channeldb

import (
	"bytes"
	"testing"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

func TestWriteExtraData(t *testing.T) {
	b := new(bytes.Buffer)

	htlc := &lnwire.UpdateAddHTLC{
		ExtraData: []byte{1, 2, 3},
	}

	err := WriteElement(b, htlc)
	require.NoError(t, err, "write")

	// Start with interface declaration so that the type matches.
	var htlcRead lnwire.Message
	htlcRead = &lnwire.UpdateAddHTLC{}

	err = ReadElement(b, &htlcRead)
	require.NoError(t, err)

	add, ok := htlcRead.(*lnwire.UpdateAddHTLC)
	require.True(t, ok, "expected add")

	require.Equal(t, htlc.ExtraData, add.ExtraData)
}
