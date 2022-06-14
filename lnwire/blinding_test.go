package lnwire

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/stretchr/testify/require"
)

const pubkeyStr = "02eec7245d6b7d2ccb30380bfbe2a3648cd7a942653f5aa340edcea1f283686619"

func pubkey(t *testing.T) *btcec.PublicKey {
	nodeBytes, err := hex.DecodeString(pubkeyStr)
	require.NoError(t, err)

	nodePk, err := btcec.ParsePubKey(nodeBytes)
	require.NoError(t, err)

	return nodePk
}

// TestBlindedDataEncoding tests encoding and decoding of blinded data blobs.
func TestBlindedDataEncoding(t *testing.T) {
	var (
		channelID = NewShortChanIDFromInt(1)
	)

	encodedData := &BlindedRouteData{
		ShortChannelID: &channelID,
		NextNodeID:     pubkey(t),
		RelayInfo: &PaymentRelayInfo{
			FeeBase:        1,
			FeeProportinal: 2,
			CltvDelta:      3,
		},
		Constraints: &PaymentConstraints{
			MaxCltvExpiry:   4,
			HtlcMinimumMsat: 5,
			AllowedFeatures: []byte{6},
		},
	}

	encoded, err := EncodeBlindedRouteData(encodedData)
	require.NoError(t, err)

	b := bytes.NewBuffer(encoded)
	decodedData, err := DecodeBlindedRouteData(b)
	require.NoError(t, err)

	require.Equal(t, encodedData, decodedData)
}
