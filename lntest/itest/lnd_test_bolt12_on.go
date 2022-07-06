//go:build bolt12
// +build bolt12

package itest

import (
	"testing"

	bnditest "github.com/carlakc/boltnd/itest"
	"github.com/lightningnetwork/lnd/lntest"
)

var allTestCases = []*testCase{
	{
		name: "onion messages",
		test: wrapExternalTest(bnditest.OnionMessageTestCase),
	},
	{
		name: "decode offer",
		test: wrapExternalTest(bnditest.DecodeOfferTestCase),
	},
	{
		name: "subscribe onion payload",
		test: wrapExternalTest(bnditest.SubscribeOnionPayload),
	},
}

// externalTest is the function signature for test cases outside of lnd that
// do not have access to our unexported harness test struct.
type externalTest func(*testing.T, *lntest.NetworkHarness)

// wrapExternalTest wraps externally provided test cases in the format (that
// uses internal types) required for test cases.
func wrapExternalTest(testCase externalTest) func(net *lntest.NetworkHarness,
	t *harnessTest) {

	return func(net *lntest.NetworkHarness, t *harnessTest) {
		testCase(t.t, net)
	}
}
