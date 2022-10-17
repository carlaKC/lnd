//go:build !dev
// +build !dev

package lncfg

import "github.com/lightningnetwork/lnd/lnwire"

// ExperimentalProtocol is a sub-config that houses any experimental protocol
// features that also require a build-tag to activate.
type ExperimentalProtocol struct {
}

// CustomMessageOverrides returns the set of protocol messages that we override
// to allow custom handling.
func (p ExperimentalProtocol) CustomMessageOverrides() []uint64 {
	return nil
}

// CustomFeatureBits returns the set of protocol feature bits that should be
// advertised.
func (p ExperimentalProtocol) CustomFeatureBits() []lnwire.FeatureBit {
	return nil
}
