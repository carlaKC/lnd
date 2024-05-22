package itest

import (
	"math"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/rpc"
	"github.com/lightningnetwork/lnd/lntest/wait"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

// testExperimentalEndorsement tests setting of positive and negative
// experimental endorsement signals.
func testExperimentalEndorsement(ht *lntest.HarnessTest) {
	testEndorsement(ht, true)
	testEndorsement(ht, false)
}

// testEndorsement sets up a 5 hop network and tests propagation of
// experimental endorsement signals.
func testEndorsement(ht *lntest.HarnessTest, aliceEndorse bool) {
	alice, bob := ht.Alice, ht.Bob
	carol := ht.NewNode(
		"carol", []string{"--protocol.no-experimental-endorsement"},
	)
	dave := ht.NewNode("dave", nil)
	eve := ht.NewNode("eve", nil)

	ht.EnsureConnected(alice, bob)
	ht.EnsureConnected(bob, carol)
	ht.EnsureConnected(carol, dave)
	ht.EnsureConnected(dave, eve)

	ht.FundCoins(btcutil.SatoshiPerBitcoin, carol)
	ht.FundCoins(btcutil.SatoshiPerBitcoin, dave)
	// Open and wait for channels.
	const chanAmt = btcutil.Amount(300000)
	p := lntest.OpenChannelParams{Amt: chanAmt}
	reqs := []*lntest.OpenChannelRequest{
		{Local: alice, Remote: bob, Param: p},
		{Local: bob, Remote: carol, Param: p},
		{Local: carol, Remote: dave, Param: p},
		{Local: dave, Remote: eve, Param: p},
	}
	resp := ht.OpenMultiChannelsAsync(reqs)
	cpAB, cpBC, cpCD, cpDE := resp[0], resp[1], resp[2], resp[3]

	// Make sure Alice is aware of Bob=>Carol=>Dave=>Eve channels.
	ht.AssertTopologyChannelOpen(alice, cpBC)
	ht.AssertTopologyChannelOpen(alice, cpCD)
	ht.AssertTopologyChannelOpen(alice, cpDE)

	bobIntercept, cancelBob := bob.RPC.HtlcInterceptor()
	defer cancelBob()

	carolIntercept, cancelCarol := carol.RPC.HtlcInterceptor()
	defer cancelCarol()

	daveIntercept, cancelDave := dave.RPC.HtlcInterceptor()
	defer cancelDave()

	req := &lnrpc.Invoice{ValueMsat: 1000}
	addResponse := eve.RPC.AddInvoice(req)
	invoice := eve.RPC.LookupInvoice(addResponse.RHash)

	sendReq := &routerrpc.SendPaymentRequest{
		PaymentRequest: invoice.PaymentRequest,
		TimeoutSeconds: int32(wait.PaymentTimeout.Seconds()),
		FeeLimitMsat:   math.MaxInt64,
	}

	expectedValue := []byte{lnwire.ExperimentalUnendorsed}
	if aliceEndorse {
		expectedValue = []byte{lnwire.ExperimentalEndorsed}
		t := uint64(lnwire.ExperimentalEndorsementType)
		sendReq.FirstHopCustomRecords = map[uint64][]byte{
			t: expectedValue,
		}
	}

	_ = alice.RPC.SendPayment(sendReq)

	// Validate that our signal (positive or zero) propagates until carol
	// and then is dropped because she has disabled the feature.
	validateEndorsedAndResume(ht, bobIntercept, 1, expectedValue)
	validateEndorsedAndResume(ht, carolIntercept, 1, expectedValue)
	validateEndorsedAndResume(ht, daveIntercept, 0, nil)

	var preimage lntypes.Preimage
	copy(preimage[:], invoice.RPreimage)
	ht.AssertPaymentStatus(alice, preimage, lnrpc.Payment_SUCCEEDED)

	ht.CloseChannel(alice, cpAB)
	ht.CloseChannel(bob, cpBC)
	ht.CloseChannel(carol, cpCD)
}

func validateEndorsedAndResume(ht *lntest.HarnessTest,
	interceptor rpc.InterceptorClient, expectedRecords int,
	expectedValue []byte) {

	packet := ht.ReceiveHtlcInterceptor(interceptor)

	require.Len(ht, packet.IncomingHtlcWireCustomRecords, expectedRecords)

	if expectedRecords == 1 {
		t := uint64(lnwire.ExperimentalEndorsementType)
		value, ok := packet.IncomingHtlcWireCustomRecords[t]
		require.True(ht, ok)

		require.Equal(ht, expectedValue, value)
	}

	err := interceptor.Send(&routerrpc.ForwardHtlcInterceptResponse{
		IncomingCircuitKey: packet.IncomingCircuitKey,
		Action:             routerrpc.ResolveHoldForwardAction_RESUME,
	})
	require.NoError(ht, err)
}
