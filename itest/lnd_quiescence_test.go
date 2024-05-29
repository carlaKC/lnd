package itest

import (
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/wait"
	"github.com/stretchr/testify/require"
)

// testQuiescence tests whether we can come to agreement on quiescence of a
// channel. We initiate quiescence via RPC and if it succeeds we verify that
// the expected initiator is the resulting initiator.
//
// NOTE FOR REVIEW: this could be improved by blasting the channel with HTLC
// traffic on both sides to increase the surface area of the change under test.
func testQuiescence(ht *lntest.HarnessTest) {
	alice, bob := ht.Alice, ht.Bob

	chanPoint := ht.OpenChannel(bob, alice, lntest.OpenChannelParams{
		Amt: btcutil.Amount(1000000),
	})
	defer ht.CloseChannel(bob, chanPoint)

	ht.AssertTopologyChannelOpen(bob, chanPoint)

	res, err := alice.RPC.Quiesce(&lnrpc.QuiescenceRequest{
		ChanId: chanPoint,
	})

	require.NoError(ht, err)
	require.True(ht, res.Initiator)
}

func testQuiescenceInFlight(ht *lntest.HarnessTest) {
	alice, bob := ht.Alice, ht.Bob
	carol := ht.NewNode("carol", nil)

	ht.EnsureConnected(alice, bob)
	ht.EnsureConnected(bob, carol)

	// Open and wait for channels.
	const chanAmt = btcutil.Amount(300000)
	p := lntest.OpenChannelParams{Amt: chanAmt}
	reqs := []*lntest.OpenChannelRequest{
		{Local: alice, Remote: bob, Param: p},
		{Local: bob, Remote: carol, Param: p},
	}
	resp := ht.OpenMultiChannelsAsync(reqs)
	cpAB, cpBC := resp[0], resp[1]

	// Make sure Alice is aware of channel Bob=>Carol.
	ht.AssertTopologyChannelOpen(alice, cpBC)

	// Connect the interceptor for bob.
	interceptor, cancelInterceptor := bob.RPC.HtlcInterceptor()
	defer cancelInterceptor()

	// Wait for alice to be aware of all channels.
	ht.AssertTopologyChannelOpen(alice, cpAB)
	ht.AssertTopologyChannelOpen(alice, cpBC)

	// Pay an invoice from Alice -> Carol.
	inv := carol.RPC.AddInvoice(
		&lnrpc.Invoice{ValueMsat: 200_000},
	)
	sendClient := alice.RPC.SendPayment(
		&routerrpc.SendPaymentRequest{
			PaymentRequest: inv.PaymentRequest,
			TimeoutSeconds: int32(wait.PaymentTimeout.Seconds()),
			FeeLimitMsat:   noFeeLimitMsat,
		},
	)

	// Wait for the payment to reach Bob's interceptor (switch).
	packet := ht.ReceiveHtlcInterceptor(interceptor)

	// Once the payment has reached Bob's interceptor, we know that there
	// is nothing pending commitment on the Alice->Bob channel so we
	// acquire quiescence.
	res, err := alice.RPC.Quiesce(&lnrpc.QuiescenceRequest{
		ChanId: cpAB,
	})

	require.NoError(ht, err)
	require.True(ht, res.Initiator)

	// Now, release the intercepted payment with failure, artificially
	// hitting the case where we have an in-mailbox packet that is set back
	// to our quiescing link _after_ we've stfu'd.
	err = interceptor.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
	require.NoError(ht, err)

	// We'll sleep for a bit, because we don't have a way to gauge when the
	// UpdateFailHtlc is received by Alice.
	time.Sleep(time.Second * 60)

	// Next, disconnect and reconnect nodes to reset quiescence.
	ht.DisconnectNodes(alice, bob)
	ht.EnsureConnected(alice, bob)

	// Finally, we assert that we're actually able to finalize this payment
	// after we are un-quiesced.
	ht.AssertPaymentStatusFromStream(sendClient, lnrpc.Payment_FAILED)

	ht.CloseChannel(alice, cpAB)
	ht.CloseChannel(bob, cpBC)
}
