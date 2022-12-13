package itest

import (
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

// testForwardBlindedRoute tests lnd's ability to forward payments in a blinded
// route.
func testForwardBlindedRoute(net *lntest.NetworkHarness, t *harnessTest) {
	carol := net.NewNode(t.t, "Carol", nil)
	defer shutdownAndAssert(net, t, carol)

	dave := net.NewNode(t.t, "Dave", nil)
	defer shutdownAndAssert(net, t, dave)

	setupFourHopNetwork(t, net, carol, dave)
}
