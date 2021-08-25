package routing

import (
	"github.com/lightningnetwork/lnd/channeldb"
	"github.com/lightningnetwork/lnd/htlcswitch"
	"github.com/lightningnetwork/lnd/kvdb"
	"github.com/lightningnetwork/lnd/lnwire"
)

// bandwidthHints provides hints about the currently available balance in our
// channels.
type bandwidthHints interface {
	// availableChanBandwidth returns the total available bandwidth for a
	// channel and a bool indicating whether the channel hint was found.
	// If the channel is unavailable, a zero amount is returned.
	availableChanBandwidth(channelID uint64) (lnwire.MilliSatoshi, bool)
}

// getLinkQuery is the function signature used to lookup a link.
type getLinkQuery func(chanID lnwire.ChannelID) (
	htlcswitch.ChannelUpdateHandler, error)

// bandwidthManager is an implementation of the bandwidthHints interface which
// uses the link lookup provided to query the link for our latest local channel
// balances.
type bandwidthManager struct {
	getLink    getLinkQuery
	localChans map[uint64]lnwire.ChannelID
}

// newBandwidthManager creates a bandwidth manager for the source node provided
// which is used to obtain hints from the lower layer w.r.t the available
// bandwidth of edges on the network. Currently, we'll only obtain bandwidth
// hints for the edges we directly have open ourselves. Obtaining these hints
// allows us to reduce the number of extraneous attempts as we can skip channels
// that are inactive, or just don't have enough bandwidth to carry the payment.
func newBandwidthManager(sourceNode *channeldb.LightningNode,
	linkQuery getLinkQuery) (*bandwidthManager, error) {

	manager := &bandwidthManager{
		getLink:    linkQuery,
		localChans: make(map[uint64]lnwire.ChannelID),
	}

	// First, we'll collect the set of outbound edges from the target
	// source node and add them to our bandwidth manager's map of channels.
	err := sourceNode.ForEachChannel(nil, func(tx kvdb.RTx,
		edgeInfo *channeldb.ChannelEdgeInfo,
		_, _ *channeldb.ChannelEdgePolicy) error {

		cid := lnwire.NewChanIDFromOutPoint(&edgeInfo.ChannelPoint)
		manager.localChans[edgeInfo.ChannelID] = cid

		return nil
	})
	if err != nil {
		return nil, err
	}

	return manager, nil
}

// getBandwidth queries the current state of a link and gets its currently
// available bandwidth. Note that this function assumes that the channel being
// queried is one of our local channels, so any failure to retrieve the link
// is interpreted as the link being offline.
func (b *bandwidthManager) getBandwidth(cid lnwire.ChannelID) lnwire.MilliSatoshi {
	link, err := b.getLink(cid)
	if err != nil {
		// If the link isn't online, then we'll report that it has
		// zero bandwidth.
		return 0
	}

	// If the link is found within the switch, but it isn't yet eligible
	// to forward any HTLCs, then we'll treat it as if it isn't online in
	// the first place.
	if !link.EligibleToForward() {
		return 0
	}

	// If our link isn't currently in a state where it can  add another
	// outgoing htlc, treat the link as unusable.
	if err := link.MayAddOutgoingHtlc(); err != nil {
		return 0
	}

	// Otherwise, we'll return the current best estimate for the available
	// bandwidth for the link.
	return link.Bandwidth()
}

// availableChanBandwidth returns the total available bandwidth for a channel
// and a bool indicating whether the channel hint was found. If the channel is
// unavailable, a zero amount is returned.
func (b *bandwidthManager) availableChanBandwidth(channelID uint64) (
	lnwire.MilliSatoshi, bool) {

	channel, ok := b.localChans[channelID]
	if !ok {
		return 0, false
	}

	return b.getBandwidth(channel), true
}
