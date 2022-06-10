package lnwire

import (
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// BlindingPointRecordType is the type for ephemeral pubkeys used in
	// route blinding.
	BlindingPointRecordType tlv.Type = 0
)

// BlindingPoint is used to communicate ephemeral pubkeys used by route
// blinding.
//
// TODO(carla): initially used type BlindingPoint btcec.PublicKey, this
// broke the TLV library's reflection because it btcec.PublicKey is
// itself an alias of secp.PublicKey. I believe that the reflection
// goes all the way down to the lowest level alias, so our type assertions
// start to break (although not sure why this doesn't happen with straight
// btcec.PublicKey). Gave up
type BlindingPoint struct {
	*btcec.PublicKey
}

// Record returns a TLV record for blinded pubkeys.
//
// Note: implements the RecordProducer interface.
func (p *BlindingPoint) Record() tlv.Record {
	return tlv.MakeDynamicRecord(
		BlindingPointRecordType, &p.PublicKey,
		func() uint64 {
			return 33
		},
		tlv.EPubKey, tlv.DPubKey,
	)
}
