package routing

import (
	"github.com/lightningnetwork/lnd/channeldb"
)

type AdditionalEdge interface {
	EdgePolicy() *channeldb.CachedEdgePolicy
	HopPayloadSize() uint64
}

type DirectedEdge struct {
	policy      *channeldb.CachedEdgePolicy
	payLoadSize uint64
}

func (edge *DirectedEdge) EdgePolicy() *channeldb.CachedEdgePolicy {
	return edge.policy
}

func (edge *DirectedEdge) HopPayloadSize() uint64 {
	return edge.payLoadSize
}
