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

	bobIntercept, cancelBob := bob.RPC.HtlcInterceptor()
	defer cancelBob()

	req := &lnrpc.Invoice{ValueMsat: 1000}
	addResponse := carol.RPC.AddInvoice(req)

	sendReq := &routerrpc.SendPaymentRequest{
		PaymentRequest: addResponse.PaymentRequest,
		TimeoutSeconds: int32(wait.PaymentTimeout.Seconds()),
		FeeLimitMsat:   noFeeLimitMsat,
		Endorsed:       routerrpc.HTLCEndorsement_ENDORSEMENT_FALSE,
	}

	paymentClient := alice.RPC.SendPayment(sendReq)

	// Receive a HTLC from alice for the payment, remember which channel it
	// wanted to depart on (we have two possible ones) and assert that it
	// isn't endorsed.
	packet := ht.ReceiveHtlcInterceptor(bobIntercept)
	originalChanOut := packet.OutgoingRequestedChanId
	require.Equal(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_FALSE,
		packet.Endorsed)

	// Now, fail the HTLC back to prompt a retry on the same path with
	// endorsement.
	err := bobIntercept.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_FAIL,
	})
	require.NoError(ht, err)

	// Wait for alice to retry the payment, this time endorsed.
	packet = ht.ReceiveHtlcInterceptor(bobIntercept)
	require.Equal(ht, originalChanOut, packet.OutgoingRequestedChanId,
		"did not retry same route")
	require.Equal(ht, routerrpc.HTLCEndorsement_ENDORSEMENT_TRUE,
		packet.Endorsed, "did not retry endorsed")

	err = bobIntercept.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_RESUME,
	})
	require.NoError(ht, err)

	ht.AssertPaymentStatusFromStream(paymentClient, lnrpc.Payment_SUCCEEDED)
}
