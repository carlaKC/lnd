import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/chainreg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/node"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
	"github.com/lightningnetwork/lnd/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testQueryBlindedRoutes tests querying routes to blinded routes. To do this,
// it sets up a nework of Alice - Bob - Carol and creates a mock blinded route
// that uses Carol as the introduction node (plus dummy hops to cover multiple
// hops). The test simply asserts that the structure of the route is as
// expected. It also includes the edge case of a single-hop blinded route,
// which indicates that the introduction node is the recipient.
func testQueryBlindedRoutes(ht *lntest.HarnessTest) {
	var (
		ctxb = context.Background()

		// Convenience aliases.
		alice = ht.Alice
		bob   = ht.Bob
	)

	// Setup a two hop channel network: Alice -- Bob -- Carol.
	// We set our proportional fee for these channels to zero, so that
	// our calculations are easier. This is okay, because we're not testing
	// the basic mechanics of pathfinding in this test.
	chanAmt := btcutil.Amount(100000)
	chanPointAliceBob := ht.OpenChannel(
		alice, bob, lntest.OpenChannelParams{
			Amt:        chanAmt,
			BaseFee:    10000,
			FeeRate:    0,
			UseBaseFee: true,
			UseFeeRate: true,
		},
	)

	carol := ht.NewNode("Carol", nil)
	ht.EnsureConnected(bob, carol)

	var bobCarolBase uint64 = 2000
	chanPointBobCarol := ht.OpenChannel(
		bob, carol, lntest.OpenChannelParams{
			Amt:        chanAmt,
			BaseFee:    bobCarolBase,
			FeeRate:    0,
			UseBaseFee: true,
			UseFeeRate: true,
		},
	)

	// Wait for Alice to see Bob/Carol's channel because she'll need it for
	// pathfinding.
	ht.AssertTopologyChannelOpen(alice, chanPointBobCarol)

	// Lookup full channel info so that we have channel ids for our route.
	aliceBobChan := ht.GetChannelByChanPoint(alice, chanPointAliceBob)
	bobCarolChan := ht.GetChannelByChanPoint(bob, chanPointBobCarol)

	// Sanity check that bob's fee is as expected.
	chanInfoReq := &lnrpc.ChanInfoRequest{
		ChanId: bobCarolChan.ChanId,
	}

	bobCarolInfo, err := bob.RPC.LN.GetChanInfo(ctxb, chanInfoReq)
	require.NoError(ht, err)

	// Our test relies on knowing the fee rate for bob - carol to set the
	// fees we expect for our route. Perform a quick sanity check that our
	// policy is as expected.
	var policy *lnrpc.RoutingPolicy
	if bobCarolInfo.Node1Pub == bob.PubKeyStr {
		policy = bobCarolInfo.Node1Policy
	} else {
		policy = bobCarolInfo.Node2Policy
	}
	require.Equal(ht, bobCarolBase, uint64(policy.FeeBaseMsat), "base fee")
	require.EqualValues(ht, 0, policy.FeeRateMilliMsat, "fee rate")

	// We'll also need the current block height to calculate our locktimes.
	ctxt, cancel := context.WithTimeout(ctxb, defaultTimeout)
	defer cancel()

	info, err := alice.RPC.LN.GetInfo(ctxt, &lnrpc.GetInfoRequest{})
	require.NoError(ht, err)
	cancel()

	// Since we created channels with default parameters, we can assume
	// that all of our channels have the default cltv delta.
	bobCarolDelta := uint32(chainreg.DefaultBitcoinTimeLockDelta)

	// Create arbitrary pubkeys for use in our blinded route. They're not
	// actually used functionally in this test, so we can just make them up.
	var (
		_, blindingPoint = btcec.PrivKeyFromBytes([]byte{1})
		_, carolBlinded  = btcec.PrivKeyFromBytes([]byte{2})
		_, blindedHop1   = btcec.PrivKeyFromBytes([]byte{3})
		_, blindedHop2   = btcec.PrivKeyFromBytes([]byte{4})

		encryptedDataCarol = []byte{1, 2, 3}
		encryptedData1     = []byte{4, 5, 6}
		encryptedData2     = []byte{7, 8, 9}

		blindingBytes     = blindingPoint.SerializeCompressed()
		carolBlindedBytes = carolBlinded.SerializeCompressed()
		blinded1Bytes     = blindedHop1.SerializeCompressed()
		blinded2Bytes     = blindedHop2.SerializeCompressed()
	)

	// Now we create a blinded route which uses carol as an introduction
	// node followed by two dummy hops (the arbitrary pubkeys in our
	// blinded route above:
	// Carol --- B1 --- B2
	route := &lnrpc.BlindedPath{
		IntroductionNode: carol.PubKey[:],
		BlindingPoint:    blindingBytes,
		BlindedHops: []*lnrpc.BlindedHop{
			{
				// The first hop in the blinded route is
				// expected to be the introduction node.
				BlindedNode:   carolBlindedBytes,
				EncryptedData: encryptedDataCarol,
			},
			{
				BlindedNode:   blinded1Bytes,
				EncryptedData: encryptedData1,
			},
			{
				BlindedNode:   blinded2Bytes,
				EncryptedData: encryptedData2,
			},
		},
	}

	// Create a blinded payment that has aggregate cltv and fee params
	// for our route.
	var (
		blindedBaseFee   uint64 = 1500
		blindedCltvDelta uint32 = 125
	)

	blindedPayment := &lnrpc.BlindedPayment{
		BlindedPath:    route,
		BaseFeeMsat:    blindedBaseFee,
		TotalCltvDelta: blindedCltvDelta,
	}

	// Query for a route to the blinded path constructed above.
	ctxt, cancel = context.WithTimeout(ctxb, defaultTimeout)
	defer cancel()

	var (
		paymentAmt int64  = 100_000
		finalDelta uint32 = 50
	)
	req := &lnrpc.QueryRoutesRequest{
		AmtMsat: paymentAmt,
		BlindedPath: []*lnrpc.BlindedPayment{
			blindedPayment,
		},
		FinalCltvDelta: int32(finalDelta),
	}

	resp, err := alice.RPC.LN.QueryRoutes(ctxt, req)
	require.NoError(ht, err)
	require.Len(ht, resp.Routes, 1)

	// Payment amount and cltv will be included for the bob/carol edge
	// (because we apply on the outgoing hop), and the blinded portion of
	// the route.
	totalFee := bobCarolBase + blindedBaseFee
	totalAmt := uint64(paymentAmt) + totalFee
	totalCltv := info.BlockHeight + bobCarolDelta + blindedCltvDelta +
		finalDelta

	// Alice -> Bob
	//   Forward: total - bob carol fees
	//   Expiry: total - bob carol delta
	//
	// Bob -> Carol
	//  Forward: 101500 (total + blinded fees)
	//  Expiry: Height + 125 (final delta)
	//  Encrypted Data: enc_carol
	//
	// Carol -> Blinded 1
	//  Forward/ Expiry: 0
	//  Encrypted Data: enc_1
	//
	// Blinded 1 -> Blinded 2
	//  Forward/ Expiry: blinded amount / expiry
	//  Encrypted Data: enc_2
	hop0Amount := int64(totalAmt - bobCarolBase)
	hop0Expiry := totalCltv - bobCarolDelta
	blindedExpiry := hop0Expiry - blindedCltvDelta

	expectedRoute := &lnrpc.Route{
		TotalTimeLock: totalCltv,
		TotalAmtMsat:  int64(totalAmt),
		TotalFeesMsat: int64(totalFee),
		Hops: []*lnrpc.Hop{
			{
				ChanId:           aliceBobChan.ChanId,
				Expiry:           hop0Expiry,
				AmtToForwardMsat: hop0Amount,
				FeeMsat:          int64(bobCarolBase),
				PubKey:           bob.PubKeyStr,
			},
			{
				ChanId:        bobCarolChan.ChanId,
				PubKey:        carol.PubKeyStr,
				BlindingPoint: blindingBytes,
				FeeMsat:       int64(blindedBaseFee),
				EncryptedData: encryptedDataCarol,
			},
			{
				PubKey: hex.EncodeToString(
					blinded1Bytes,
				),
				EncryptedData: encryptedData1,
			},
			{
				PubKey: hex.EncodeToString(
					blinded2Bytes,
				),
				AmtToForwardMsat: paymentAmt,
				Expiry:           blindedExpiry,
				EncryptedData:    encryptedData2,
			},
		},
	}

	r := resp.Routes[0]
	assert.Equal(ht, expectedRoute.TotalTimeLock, r.TotalTimeLock)
	assert.Equal(ht, expectedRoute.TotalAmtMsat, r.TotalAmtMsat)
	assert.Equal(ht, expectedRoute.TotalFeesMsat, r.TotalFeesMsat)

	assert.Equal(ht, len(expectedRoute.Hops), len(r.Hops))
	for i, hop := range expectedRoute.Hops {
		assert.Equal(ht, hop.PubKey, r.Hops[i].PubKey,
			"hop: %v pubkey", i)

		assert.Equal(ht, hop.ChanId, r.Hops[i].ChanId,
			"hop: %v chan id", i)

		assert.Equal(ht, hop.Expiry, r.Hops[i].Expiry,
			"hop: %v expiry", i)

		assert.Equal(ht, hop.AmtToForwardMsat,
			r.Hops[i].AmtToForwardMsat, "hop: %v forward", i)

		assert.Equal(ht, hop.FeeMsat, r.Hops[i].FeeMsat,
			"hop: %v fee", i)

		assert.Equal(ht, hop.BlindingPoint, r.Hops[i].BlindingPoint,
			"hop: %v blinding point", i)

		assert.Equal(ht, hop.EncryptedData, r.Hops[i].EncryptedData,
			"hop: %v encrypted data", i)
	}

	// Dispatch a payment to our blinded route.
	preimage := [33]byte{1, 2, 3}
	hash := sha256.Sum256(preimage[:])

	sendReq := &routerrpc.SendToRouteRequest{
		PaymentHash: hash[:],
		Route:       r,
	}

	ctxt, cancel = context.WithTimeout(ctxb, defaultTimeout)
	htlcAttempt, err := alice.RPC.Router.SendToRouteV2(ctxt, sendReq)
	cancel()
	require.NoError(ht, err)

	// Since Carol doesn't understand blinded routes, we expect her to fail
	// the payment because the onion payload is invalid (missing amount to
	// forward).
	require.NotNil(ht, htlcAttempt.Failure)
	require.Equal(ht, uint32(2), htlcAttempt.Failure.FailureSourceIndex)

	// Next, we test an edge case where just an introduction node is
	// included as a "single hop blinded route".
	introNodeBlinded := &lnrpc.BlindedPayment{
		BlindedPath: &lnrpc.BlindedPath{
			IntroductionNode: carol.PubKey[:],
			BlindingPoint:    blindingBytes,
			BlindedHops: []*lnrpc.BlindedHop{
				{
					// The first hop in the blinded route is
					// expected to be the introduction node.
					BlindedNode:   carolBlindedBytes,
					EncryptedData: encryptedDataCarol,
				},
			},
		},
		// Fees and expiry are zero for single-hop blinded paths.
		BaseFeeMsat:    0,
		TotalCltvDelta: 0,
	}
	req = &lnrpc.QueryRoutesRequest{
		AmtMsat: paymentAmt,
		BlindedPath: []*lnrpc.BlindedPayment{
			introNodeBlinded,
		},
		FinalCltvDelta: int32(finalDelta),
	}

	ctxt, cancel = context.WithTimeout(ctxb, defaultTimeout)
	resp, err = alice.RPC.LN.QueryRoutes(ctxt, req)
	cancel()
	require.NoError(ht, err)

	// Assert that we have one route, and two hops: Alice/Bob and Bob/Carol.
	require.Len(ht, resp.Routes, 1)
	require.Len(ht, resp.Routes[0].Hops, 2)

	ht.CloseChannel(alice, chanPointAliceBob)
	ht.CloseChannel(bob, chanPointBobCarol)
}

type blindedForwardTest struct {
	ht       *lntest.HarnessTest
	carol    *node.HarnessNode
	dave     *node.HarnessNode
	channels []*lnrpc.ChannelPoint

	preimage [33]byte

	// ctx is a context to be used by the test.
	ctx context.Context //nolint:containedctx

	// cancel will cancel the test's top level context.
	cancel func()
}

func newBlindedForwardTest(ht *lntest.HarnessTest) *blindedForwardTest {
	ctx, cancel := context.WithCancel(context.Background())

	return &blindedForwardTest{
		ht:       ht,
		ctx:      ctx,
		cancel:   cancel,
		preimage: [33]byte{1, 2, 3},
	}
}

// setup spins up additional nodes needed for our test and creates a four hop
// network for testing blinded forwarding and returns a blinded route from
// Bob -> Carol -> Dave, with Bob acting as the introduction point.
func (b *blindedForwardTest) setup() *routing.BlindedPayment {
	b.carol = b.ht.NewNode("Carol", nil)
	b.dave = b.ht.NewNode("Dave", nil)

	b.channels = setupFourHopNetwork(b.ht, b.carol, b.dave)

	// Create a blinded route to Dave via Bob --- Carol --- Dave:
	bobChan := b.ht.GetChannelByChanPoint(b.ht.Bob, b.channels[1])
	carolChan := b.ht.GetChannelByChanPoint(b.carol, b.channels[2])

	edges := []*forwardingEdge{
		getForwardingEdge(b.ctx, b.ht, b.ht.Bob, bobChan.ChanId),
		getForwardingEdge(b.ctx, b.ht, b.carol, carolChan.ChanId),
	}

	davePk, err := btcec.ParsePubKey(b.dave.PubKey[:])
	require.NoError(b.ht, err, "dave pubkey")

	return b.createBlindedRoute(edges, davePk)
}

// cleanup tears down all channels created by the test and cancels the top
// level context used in the test.
func (b *blindedForwardTest) cleanup() {
	b.ht.CloseChannel(b.ht.Alice, b.channels[0])
	b.ht.CloseChannel(b.ht.Bob, b.channels[1])
	b.ht.CloseChannel(b.carol, b.channels[2])

	b.cancel()
}

// createRouteToBlinded queries for a route from alice to the blinded path
// provided.
//
//nolint:gomnd
func (b *blindedForwardTest) createRouteToBlinded(paymentAmt int64,
	route *routing.BlindedPayment) *lnrpc.Route {

	intro := route.BlindedPath.IntroductionPoint.SerializeCompressed()
	blinding := route.BlindedPath.BlindingPoint.SerializeCompressed()

	blindedRoute := &lnrpc.BlindedPath{
		IntroductionNode: intro,
		BlindingPoint:    blinding,
		BlindedHops: make(
			[]*lnrpc.BlindedHop,
			len(route.BlindedPath.BlindedHops),
		),
	}

	for i, hop := range route.BlindedPath.BlindedHops {
		blindedRoute.BlindedHops[i] = &lnrpc.BlindedHop{
			BlindedNode:   hop.NodePub.SerializeCompressed(),
			EncryptedData: hop.Payload,
		}
	}
	blindedPath := &lnrpc.BlindedPayment{
		BlindedPath: blindedRoute,
		BaseFeeMsat: uint64(
			route.BaseFee,
		),
		ProportionalFeeMsat: uint64(
			route.ProportionalFee,
		),
		TotalCltvDelta: uint32(
			route.CltvExpiryDelta,
		),
	}

	ctxt, cancel := context.WithTimeout(b.ctx, defaultTimeout)
	req := &lnrpc.QueryRoutesRequest{
		AmtMsat: paymentAmt,
		// Our fee limit doesn't really matter, we just want to
		// be able to make the payment.
		FeeLimit: &lnrpc.FeeLimit{
			Limit: &lnrpc.FeeLimit_Percent{
				Percent: 50,
			},
		},
		BlindedPath: []*lnrpc.BlindedPayment{
			blindedPath,
		},
	}

	resp, err := b.ht.Alice.RPC.LN.QueryRoutes(ctxt, req)
	cancel()
	require.NoError(b.ht, err, "query routes")
	require.Greater(b.ht, len(resp.Routes), 0, "no routes")
	require.Len(b.ht, resp.Routes[0].Hops, 3, "unexpected route length")

	return resp.Routes[0]
}

// sendBlindedPayment dispatches a payment to the route provided. The streaming
// client for the send is returned with a cancel function that can be used to
// terminate the stream.
func (b *blindedForwardTest) sendBlindedPayment(route *lnrpc.Route) (
	lnrpc.Lightning_SendToRouteClient, func()) {

	hash := sha256.Sum256(b.preimage[:])

	ctxt, cancel := context.WithCancel(b.ctx)
	sendReq := &lnrpc.SendToRouteRequest{
		PaymentHash: hash[:],
		Route:       route,
	}

	sendClient, err := b.ht.Alice.RPC.LN.SendToRoute(ctxt)
	require.NoError(b.ht, err, "send to route client")

	err = sendClient.SendMsg(sendReq)
	require.NoError(b.ht, err, "send to route request")

	return sendClient, cancel
}

// setupFourHopNetwork creates a network with the following topology and
// liquidity:
// Alice (100k)----- Bob (100k) ----- Carol (100k) ----- Dave
//
// The funding outpoint for AB / BC / CD are returned in-order.
func setupFourHopNetwork(ht *lntest.HarnessTest,
	carol, dave *node.HarnessNode) []*lnrpc.ChannelPoint {

	const chanAmt = btcutil.Amount(100000)
	var networkChans []*lnrpc.ChannelPoint

	// Open a channel with 100k satoshis between Alice and Bob with Alice
	// being the sole funder of the channel.
	chanPointAlice := ht.OpenChannel(
		ht.Alice, ht.Bob, lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointAlice)

	// Create a channel between bob and carol.
	ht.EnsureConnected(ht.Bob, carol)
	chanPointBob := ht.OpenChannel(
		ht.Bob, carol, lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointBob)

	// Fund carol and connect her and dave so that she can create a channel
	// between them.
	ht.FundCoins(btcutil.SatoshiPerBitcoin, carol)
	ht.EnsureConnected(carol, dave)

	chanPointCarol := ht.OpenChannel(
		carol, dave, lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	networkChans = append(networkChans, chanPointCarol)

	// Wait for all nodes to have seen all channels.
	nodes := []*node.HarnessNode{ht.Alice, ht.Bob, carol, dave}
	for _, chanPoint := range networkChans {
		for _, node := range nodes {
			ht.AssertTopologyChannelOpen(node, chanPoint)
		}
	}

	return []*lnrpc.ChannelPoint{
		chanPointAlice,
		chanPointBob,
		chanPointCarol,
	}
}

// createBlindedRoute creates a blinded route to the recipient node provided.
// The set of hops is expected to start at the introduction node and end at
// the recipient.
func (b *blindedForwardTest) createBlindedRoute(hops []*forwardingEdge,
	dest *btcec.PublicKey) *routing.BlindedPayment {

	// Create a path with space for each of our hops + the destination
	// node.
	blindedPayment := &routing.BlindedPayment{}
	pathLength := len(hops) + 1
	blindedPath := make([]*sphinx.UnBlindedHopInfo, pathLength)

	for i := 0; i < len(hops); i++ {
		node := hops[i]
		payload := &record.BlindedRouteData{
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
		delta := uint16(node.edge.TimeLockDelta)
		payload.RelayInfo = &record.PaymentRelayInfo{
			BaseFee:         uint32(node.edge.FeeBaseMsat),
			FeeRate:         uint32(node.edge.FeeRateMilliMsat),
			CltvExpiryDelta: delta,
		}

		// We set our constraints with our edge's actual htlc min, and
		// an arbitrary maximum expiry (since it's just an anti-probing
		// mechanism).
		payload.Constraints = &record.PaymentConstraints{
			HtlcMinimumMsat: lnwire.MilliSatoshi(node.edge.MinHtlc),
			MaxCltvExpiry:   100000,
		}

		blindedPayment.BaseFee += payload.RelayInfo.BaseFee
		blindedPayment.ProportionalFee += payload.RelayInfo.FeeRate
		blindedPayment.CltvExpiryDelta += delta

		// Encode the route's blinded data and include it in the
		// blinded hop.
		payloadBytes, err := record.EncodeBlindedRouteData(payload)
		require.NoError(b.ht, err)

		blindedPath[i] = &sphinx.UnBlindedHopInfo{
			NodePub: node.pubkey,
			Payload: payloadBytes,
		}
	}

	// Add our destination node at the end of the path. We don't need to
	// add any forwarding parameters because we're at the final hop.
	payloadBytes, err := record.EncodeBlindedRouteData(
		&record.BlindedRouteData{
			// TODO: we don't have support for the final hop fields,
			// because only forwarding is supported. We add a next
			// node ID here so that it _looks like_ a valid
			// forwarding hop (though in reality it's the last
			// hop).
			NextNodeID: dest,
		},
	)
	require.NoError(b.ht, err, "final payload")

	blindedPath[pathLength-1] = &sphinx.UnBlindedHopInfo{
		NodePub: dest,
		Payload: payloadBytes,
	}

	// Blind the path.
	blindingKey, err := btcec.NewPrivateKey()
	require.NoError(b.ht, err)

	blindedPayment.BlindedPath, err = sphinx.BuildBlindedPath(
		blindingKey, blindedPath,
	)
	require.NoError(b.ht, err, "build blinded path")

	return blindedPayment
}

// forwardingEdge contains the channel id/source public key for a forwarding
// edge and the policy associated with the channel in that direction.
type forwardingEdge struct {
	pubkey    *btcec.PublicKey
	channelID lnwire.ShortChannelID
	edge      *lnrpc.RoutingPolicy
}

func getForwardingEdge(ctxb context.Context, ht *lntest.HarnessTest,
	node *node.HarnessNode, chanID uint64) *forwardingEdge {

	ctxt, cancel := context.WithTimeout(ctxb, defaultTimeout)
	chanInfo, err := node.RPC.LN.GetChanInfo(ctxt, &lnrpc.ChanInfoRequest{
		ChanId: chanID,
	})
	cancel()
	require.NoError(ht, err, "%v chan info", node.Cfg.Name)

	pubkey, err := btcec.ParsePubKey(node.PubKey[:])
	require.NoError(ht, err, "%v pubkey", node.Cfg.Name)

	fwdEdge := &forwardingEdge{
		pubkey:    pubkey,
		channelID: lnwire.NewShortChanIDFromInt(chanID),
	}

	if chanInfo.Node1Pub == node.PubKeyStr {
		fwdEdge.edge = chanInfo.Node1Policy
	} else {
		require.Equal(ht, node.PubKeyStr, chanInfo.Node2Pub,
			"policy edge sanity check")

		fwdEdge.edge = chanInfo.Node2Policy
	}

	return fwdEdge
}

// testForwardBlindedRoute tests lnd's ability to forward payments in a blinded
// route.
func testForwardBlindedRoute(ht *lntest.HarnessTest) {
	testCase := newBlindedForwardTest(ht)
	defer testCase.cleanup()

	route := testCase.setup()
	blindedRoute := testCase.createRouteToBlinded(100_000, route)

	testCase.sendBlindedPayment(blindedRoute)

	// Wait for the HTLC to be active on Alice's channel. 
	hash := sha256.Sum256(testCase.preimage[:])
	ht.AssertHLTCNotActive(ht.Alice, testCase.channels[0], hash)
}