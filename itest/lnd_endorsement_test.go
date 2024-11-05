package itest

import (
	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/wait"
	"github.com/stretchr/testify/require"
)

// testEndorsement sets up a 5 hop network and tests propagation of
// experimental endorsement signals.
func testEndorsementRetry(ht *lntest.HarnessTest) {
	testEndorsement(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_UNKNOWN)
	testEndorsement(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_FALSE)
	testEndorsement(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_TRUE)
}

func testEndorsement(ht *lntest.HarnessTest,
	requestedEndorsement routerrpc.HTLCEndorsement) {

	alice, bob := ht.Alice, ht.Bob
	carol := ht.NewNode("carol", nil)

	ht.EnsureConnected(alice, bob)
	ht.EnsureConnected(bob, carol)

	ht.FundCoins(btcutil.SatoshiPerBitcoin, carol)

	// Open and wait for channels.
	const chanAmt = btcutil.Amount(300000)
	p := lntest.OpenChannelParams{Amt: chanAmt}
	reqs := []*lntest.OpenChannelRequest{
		{Local: alice, Remote: bob, Param: p},
		// Note: two channels from Bob -> Carol so that we can make
		// distinct pathfinding attempts over both.
		{Local: bob, Remote: carol, Param: p},
	}
	resp := ht.OpenMultiChannelsAsync(reqs)
	cpAB, cpBC1 := resp[0], resp[1]

	// Open separately because bob can only have one pending channel at a
	// time.
	cpBC2 := ht.OpenChannel(bob, carol, lntest.OpenChannelParams{
		Amt: btcutil.Amount(1000000),
	})

	defer ht.CloseChannel(alice, cpAB)
	defer ht.CloseChannel(bob, cpBC1)
	defer ht.CloseChannel(bob, cpBC2)

	// Make sure Alice is aware of Bob=>Carol x2.
	ht.AssertTopologyChannelOpen(alice, cpAB)
	ht.AssertTopologyChannelOpen(alice, cpBC1)
	ht.AssertTopologyChannelOpen(alice, cpBC2)

	bobIntercept, cancelBobInterceptor := bob.RPC.HtlcInterceptor()
	defer cancelBobInterceptor()

	req := &lnrpc.Invoice{ValueMsat: 20000000}
	addResponse := carol.RPC.AddInvoice(req)

	sendReq := &routerrpc.SendPaymentRequest{
		PaymentRequest: addResponse.PaymentRequest,
		TimeoutSeconds: int32(wait.PaymentTimeout.Seconds()),
		FeeLimitMsat:   noFeeLimitMsat,
		Endorsed:       requestedEndorsement,
	}

	paymentClient := alice.RPC.SendPayment(sendReq)

	// Receive a HTLC from alice for the payment, remember which channel it
	// wanted to depart on (we have two possible ones) and assert that it
	// isn't endorsed.
	packet := ht.ReceiveHtlcInterceptor(bobIntercept)
	originalChanOut := packet.OutgoingRequestedChanId
	originalAmt := packet.OutgoingAmountMsat

	// By default we expect unendorsed HTLCs, unless the user set a desired
	// value.
	expectedEndorsment := routerrpc.HTLCEndorsement_ENDORSEMENT_FALSE
	if requestedEndorsement != routerrpc.HTLCEndorsement_ENDORSEMENT_UNKNOWN {
		expectedEndorsment = requestedEndorsement
	}

	require.Equal(ht, expectedEndorsment, packet.Endorsed)

	// Fail back the HTLC to prompt retry logic.
	err := bobIntercept.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
	require.NoError(ht, err)

	// Wait for alice to retry the payment, this time endorsed.
	packet = ht.ReceiveHtlcInterceptor(bobIntercept)

	// If we did not request an endorsement status, we expect the payment
	// to be retried (this time endorsed) on the same channel.
	//
	// If the user did request a specific status, we expect them to try
	// with another route (ie, our other channel) and the same endorsement
	// status as last time.
	if requestedEndorsement == routerrpc.HTLCEndorsement_ENDORSEMENT_UNKNOWN {
		require.Equal(ht, originalChanOut, packet.OutgoingRequestedChanId,
			"did not retry same route")
		require.Equal(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_TRUE,
			packet.Endorsed, "did not retry endorsed")

	} else {
		checkPathChanged(ht, originalAmt, requestedEndorsement,
			packet, originalChanOut)

	}

	err = bobIntercept.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_RESUME,
	})
	require.NoError(ht, err)

	// If we got a new path and it was MPP, there will be another HTLC for
	// the interceptor. Cancel it here so that the payment can complete.
	cancelBobInterceptor()

	ht.AssertPaymentStatusFromStream(paymentClient, lnrpc.Payment_SUCCEEDED)
}

// checkPathChanged asserts that we have a different path than previously -
// either over a new channel, or a MPP split.
func checkPathChanged(ht *lntest.HarnessTest, originalAmt uint64,
	requestedEndorsement routerrpc.HTLCEndorsement,
	packet *routerrpc.ForwardHtlcInterceptRequest, originalChan uint64) {

	require.Equal(ht, requestedEndorsement,
		packet.Endorsed, "did not retry endorsed")

	// If there's a different channel id, then we're definitely on a new
	// path.
	if packet.OutgoingRequestedChanId != originalChan {
		return
	}

	require.NotEqual(ht, originalAmt, packet.OutgoingAmountMsat)
}
