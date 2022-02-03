package itest

import (
	"context"
	"crypto/rand"

	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/invoicesrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntypes"
	"github.com/stretchr/testify/require"
)

// testHoldHopHints tests creation of a hold invoice with hop hints included
// when a node has a large number of private channels that need hop hints.
func testHoldHopHints(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb := context.Background()

	// For Alice to include private channels in her invoices, the node that
	// she has private channels with (Bob) needs to be "public" to the
	// network, meaning that he has at least one public channel. We create
	// carol and open a Bob->Carol channel to ensure that Bob is considered
	// public when we select our hop hints later on.
	carol := net.NewNode(t.t, "Carol", nil)
	defer shutdownAndAssert(net, t, carol)

	// Connect Bob to Carol.
	net.ConnectNodes(t.t, net.Bob, carol)

	// Open a channel and wait for everybody to see it.
	chanBobCarol := openChannelAndAssert(
		t, net, net.Bob, carol,
		lntest.OpenChannelParams{
			Amt: 500000,
		},
	)

	// Wait for Bob and Carol to receive the new edge from the funding
	// manager.
	err := net.Bob.WaitForNetworkChannelOpen(chanBobCarol)
	require.NoError(t.t, err, "Bob didn't see the bob->carol "+
		"channel before timeout", err)

	err = carol.WaitForNetworkChannelOpen(chanBobCarol)
	require.NoError(t.t, err, "Carol didn't see the bob->carol "+
		"channel before timeout", err)

	// Wait for Alice to see the channel.
	err = net.Alice.WaitForNetworkChannelOpen(chanBobCarol)
	require.NoError(t.t, err, "Alice didn't see the bob->carol "+
		"channel before timeout", err)

	const (
		chanAmt     = btcutil.Amount(1000000)
		numChannels = 500
	)

	for i := 0; i < numChannels; i++ {
		t.Log("Creating channel: ", i)

		// Open a private channel.
		chanPointAlice := openChannelAndAssert(
			t, net, net.Bob, net.Alice,
			lntest.OpenChannelParams{
				Amt:     chanAmt,
				Private: true,
			},
		)

		// Wait for Alice and Bob to receive the channel edge from the
		// funding manager.
		err := net.Alice.WaitForNetworkChannelOpen(chanPointAlice)
		if err != nil {
			t.Fatalf("alice didn't see the alice->bob channel "+
				"before timeout: %v", err)
		}

		err = net.Bob.WaitForNetworkChannelOpen(chanPointAlice)
		if err != nil {
			t.Fatalf("bob didn't see the alice->bob channel "+
				"before timeout: %v", err)
		}

	}

	var preimage lntypes.Preimage
	_, err = rand.Read(preimage[:])
	require.NoError(t.t, err, "unable to generate preimage")

	payHash := preimage.Hash()

	// Create a private invoice with an amount less than our channel
	// balances so that we'll consider all of our channels for hop hints.
	invoiceReq := &invoicesrpc.AddHoldInvoiceRequest{
		Memo:    "testing",
		Value:   int64(100),
		Hash:    payHash[:],
		Private: true,
	}

	t.Log("Creating invoice")
	ctxt, _ := context.WithTimeout(ctxb, defaultTimeout)
	resp, err := net.Alice.AddHoldInvoice(ctxt, invoiceReq)
	require.NoError(t.t, err, "could not create invoice")

	req := &lnrpc.PayReqString{
		PayReq: resp.PaymentRequest,
	}

	ctxt, _ = context.WithTimeout(ctxb, defaultTimeout)
	invoice, err := net.Alice.DecodePayReq(ctxt, req)
	require.NoError(t.t, err, "could not decode invoice")

	t.Log("Created invoice ", req, len(invoice.RouteHints))

	require.True(t.t, len(invoice.RouteHints) > 0, "expected hop hints")
}
