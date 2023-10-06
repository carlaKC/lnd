package routing

import (
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/lnwire"
)

type DirectedEdge struct {
	policy          *channeldb.CachedEdgePolicy
	payloadSizeFunc customEdgeSizeFunc
}

func (edge *DirectedEdge) EdgePolicy() *channeldb.CachedEdgePolicy {
	return edge.policy
}

func (edge *DirectedEdge) HopPayloadSize(amount lnwire.MilliSatoshi, expiry uint32,
	legacy bool, channelID uint64) uint64 {

	return edge.payloadSizeFunc(amount, expiry, legacy, channelID)
}
