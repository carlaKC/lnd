package lnwire

import (
	"bytes"
	"github.com/btcsuite/btcd/wire"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/stretchr/testify/require"
)

func TestEncoding(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	require.NoError(t, err)

	pubkey := priv.PubKey()
	add := &UpdateAddHTLC{
		ChanID: NewChanIDFromOutPoint(&wire.OutPoint{}),
		BlindingPoint: BlindingPoint{
			PublicKey: pubkey,
		},
	}

	buf := new(bytes.Buffer)

	err = add.Encode(buf, 0)
	require.NoError(t, err, "encode")

	newAdd := &UpdateAddHTLC{}

	err = newAdd.Decode(buf, 0)
	require.NoError(t, err, "decode")

	require.Equal(t, pubkey, newAdd.BlindingPoint.PublicKey)
}
