package hop

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightningnetwork/lnd/lnwire"
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
		channelID = lnwire.NewShortChanIDFromInt(1)
	)

	encodedData := &blindedRouteData{
		shortChannelID: &channelID,
		nextNodeID:     pubkey(t),
		relayInfo: &paymentRelayInfo{
			feeBase:         1,
			feeProportional: 2,
			cltvDelta:       3,
		},
		constraints: &paymentConstraints{
			maxCltv:         4,
			htlcMinimum:     5,
			allowedFeatures: []byte{6},
		},
	}

	buf := new(bytes.Buffer)

	err := encodeBlindedRouteData(buf, encodedData)
	require.NoError(t, err)

	decodedData, err := decodeBlindedRouteData(buf)
	require.NoError(t, err)

	require.Equal(t, encodedData, decodedData)
}
