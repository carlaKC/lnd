package itest

import (
	"context"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

// PaymentRelay describes the relay policy for a blinded hop.
type PaymentRelay struct {
	// CltvExpiryDelta is the expiry delta for the payment hop.
	CltvExpiryDelta uint16

	// BaseFee is the per-htlc fee charged.
	BaseFee uint32

	// FeeRate is the fee rate that will be charged per millionth of a
	// satoshi.
	FeeRate uint32
}

// BlindedPayment provides the path and payment parameters required to send a
// payment along a blinded path.
type BlindedPayment struct {
	// BlindedPath contains the unblinded introduction point and blinded
	// hops for the blinded section of the payment.
	BlindedPath *sphinx.BlindedPath

	// AggregateConstraints are the payment constraints for the full
	// blinded section of the route (ie, after the introduction node).
	AggregateConstraints *lnwire.PaymentConstraints

	// AggregateRelay are the aggregated relay parameters for the full
	// blinded section of the route (ie, after the introduction node).
	AggregateRelay *lnwire.PaymentRelayInfo
}

// setupFourHopNetwork creates a network with the following topology and
// liquidity:
// Alice (100k)----- Bob (100k) ----- Carol (100k) ----- Dave
//
// The funding outpoint for AB / BC / CD are returned in-order.
func setupFourHopNetwork(t *harnessTest, net *lntest.NetworkHarness,
	carol, dave *lntest.HarnessNode) []lnwire.ShortChannelID {

	const chanAmt = btcutil.Amount(100000)
	var networkChans []*lnrpc.ChannelPoint

	// Open a channel with 100k satoshis between Alice and Bob with Alice
	// being the sole funder of the channel.
	chanPointAlice := openChannelAndAssert(
		t, net, net.Alice, net.Bob,
		lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointAlice)

	aliceChan, err := getChanInfo(net.Alice)
	require.NoError(t.t, err, "alice channel")

	// Create a channel between bob and carol.
	t.lndHarness.EnsureConnected(t.t, net.Bob, carol)
	chanPointBob := openChannelAndAssert(
		t, net, net.Bob, carol,
		lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointBob)

	// Our helper function expects one channel, so we lookup using carol.
	bobChan, err := getChanInfo(carol)
	require.NoError(t.t, err, "bob channel")

	// Fund carol and connect her and dave so that she can create a channel
	// between them.
	net.SendCoins(t.t, btcutil.SatoshiPerBitcoin, carol)
	net.ConnectNodes(t.t, carol, dave)

	t.lndHarness.EnsureConnected(t.t, carol, dave)
	chanPointCarol := openChannelAndAssert(
		t, net, carol, dave,
		lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointCarol)

	// As above, we use the helper that only expects one channel so we
	// lookup on dave's end.
	carolChan, err := getChanInfo(dave)
	require.NoError(t.t, err, "carol chan")

	// Wait for all nodes to have seen all channels.
	nodes := []*lntest.HarnessNode{net.Alice, net.Bob, carol, dave}
	nodeNames := []string{"Alice", "Bob", "Carol", "Dave"}
	for _, chanPoint := range networkChans {
		for i, node := range nodes {
			txid, err := lnrpc.GetChanPointFundingTxid(chanPoint)
			require.NoError(t.t, err, "unable to get txid")

			point := wire.OutPoint{
				Hash:  *txid,
				Index: chanPoint.OutputIndex,
			}

			err = node.WaitForNetworkChannelOpen(chanPoint)
			require.NoError(t.t, err, "%s(%d): timeout waiting for "+
				"channel(%s) open", nodeNames[i],
				node.NodeID, point)
		}
	}

	return []lnwire.ShortChannelID{
		lnwire.NewShortChanIDFromInt(aliceChan.ChanId),
		lnwire.NewShortChanIDFromInt(bobChan.ChanId),
		lnwire.NewShortChanIDFromInt(carolChan.ChanId),
	}
}

// createBlindedRoute creates a blinded route to the recipient node provided.
// The set of hops is expected to start at the introduction node and end at
// the recipient.
func createBlindedRoute(t *harnessTest,
	hops []*forwardingEdge, dest *btcec.PublicKey) *BlindedPayment {

	var (
		aggregateRelay       = &lnwire.PaymentRelayInfo{}
		aggregateConstraints = &lnwire.PaymentConstraints{}
	)

	blindingKey, err := btcec.NewPrivateKey()
	require.NoError(t.t, err)

	// Create a path with space for each of our hops + the destination
	// node.
	pathLength := len(hops) + 1
	blindedPath := make([]*sphinx.BlindedPathHop, pathLength)

	for i := 0; i < len(hops); i++ {
		node := hops[i]
		payload := &lnwire.BlindedRouteData{
			NextNodeID:     node.pubkey,
			ShortChannelID: &node.channelID,
		}

		// Add the next hop's ID for all nodes that have a next hop.
		if i < len(hops)-1 {
			nextHop := hops[i+1]

			payload.NextNodeID = nextHop.pubkey
			payload.ShortChannelID = &node.channelID
		}

		// Set the relay information for this edge, and add it to our
		// aggregate info and update our aggregate constraints.
		payload.RelayInfo = &lnwire.PaymentRelayInfo{
			FeeBase:        uint32(node.edge.FeeBaseMsat),
			FeeProportinal: uint32(node.edge.FeeRateMilliMsat),
			CltvDelta:      uint16(node.edge.TimeLockDelta),
		}

		payload.Constraints = &lnwire.PaymentConstraints{
			HtlcMinimumMsat: lnwire.MilliSatoshi(node.edge.MinHtlc),
		}

		aggregateRelay.FeeBase += payload.RelayInfo.FeeBase
		aggregateRelay.FeeProportinal += payload.RelayInfo.FeeProportinal
		aggregateRelay.CltvDelta += payload.RelayInfo.CltvDelta

		if payload.Constraints.HtlcMinimumMsat <
			aggregateConstraints.HtlcMinimumMsat {

			aggregateConstraints.HtlcMinimumMsat =
				payload.Constraints.HtlcMinimumMsat
		}

		// Encode the route's blinded data and include it in the
		// blinded hop.
		payloadBytes, err := lnwire.EncodeBlindedRouteData(payload)
		require.NoError(t.t, err)

		blindedPath[i] = &sphinx.BlindedPathHop{
			NodePub: node.pubkey,
			Payload: payloadBytes,
		}
	}

	// Add our destination node at the end of the path. We don't need to
	// add any forwarding parameters because we're at the final hop.
	payloadBytes, err := lnwire.EncodeBlindedRouteData(
		&lnwire.BlindedRouteData{
			// TODO: We shouldn't have a next node ID here (but
			// path ID isn't yet supported).
			NextNodeID: dest,
		},
	)
	require.NoError(t.t, err, "final payload")

	blindedPath[pathLength-1] = &sphinx.BlindedPathHop{
		NodePub: dest,
		Payload: payloadBytes,
	}

	blinded, err := sphinx.BuildBlindedPath(blindingKey, blindedPath)
	require.NoError(t.t, err, "build blinded path")

	return &BlindedPayment{
		BlindedPath:          blinded,
		AggregateRelay:       aggregateRelay,
		AggregateConstraints: aggregateConstraints,
	}
}

// forwardingEdge contains the channel id/source public key for a forwarding
// edge and the policy associated with the channel in that direction.
type forwardingEdge struct {
	pubkey    *btcec.PublicKey
	channelID lnwire.ShortChannelID
	edge      *lnrpc.RoutingPolicy
}

func getForwardingEdge(ctxb context.Context, t *harnessTest,
	node *lntest.HarnessNode, chanID uint64) *forwardingEdge {

	ctxt, cancel := context.WithTimeout(ctxb, defaultTimeout)
	chanInfo, err := node.GetChanInfo(ctxt, &lnrpc.ChanInfoRequest{
		ChanId: chanID,
	})
	cancel()
	require.NoError(t.t, err, "%v chan info", node.Cfg.Name)

	pubkey, err := btcec.ParsePubKey(node.PubKey[:])
	require.NoError(t.t, err, "%v pubkey", node.Cfg.Name)

	fwdEdge := &forwardingEdge{
		pubkey:    pubkey,
		channelID: lnwire.NewShortChanIDFromInt(chanID),
	}

	if chanInfo.Node1Pub == node.PubKeyStr {
		fwdEdge.edge = chanInfo.Node1Policy
	} else {
		require.Equal(t.t, node.PubKeyStr, chanInfo.Node2Pub,
			"policy edge sanity check")

		fwdEdge.edge = chanInfo.Node2Policy
	}

	return fwdEdge
}

// testForwardBlindedRoute tests lnd's ability to forward payments in a blinded
// route.
func testForwardBlindedRoute(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb := context.Background()

	carol := net.NewNode(t.t, "Carol", nil)
	defer shutdownAndAssert(net, t, carol)

	dave := net.NewNode(t.t, "Dave", nil)
	defer shutdownAndAssert(net, t, dave)

	chans := setupFourHopNetwork(t, net, carol, dave)

	// Create a blinded route to Dave via Bob --- Carol --- Dave:
	edges := []*forwardingEdge{
		getForwardingEdge(ctxb, t, net.Bob, chans[1].ToUint64()),
		getForwardingEdge(ctxb, t, carol, chans[2].ToUint64()),
	}

	davePk, err := btcec.ParsePubKey(dave.PubKey[:])
	require.NoError(t.t, err, "dave pubkey")

	createBlindedRoute(t, edges, davePk)
}
