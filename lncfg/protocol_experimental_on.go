//go:build dev
// +build dev

package lncfg

import "github.com/lightningnetwork/lnd/lnwire"

// ExperimentalProtocol is a sub-config that houses any experimental protocol
// features that also require a build-tag to activate.
type ExperimentalProtocol struct {
	CustomMessage []uint64 `long:"custom-message" description:"allows the custom message apis to send and report messages with the protocol number provided that fall outside of the custom message number range."`

	CustomFeature []uint16 `long:"custom-feature" description:"allows custome feature bits to be advertized by the node."`
}

// CustomMessageOverrides returns the set of protocol messages that we override
// to allow custom handling.
func (p ExperimentalProtocol) CustomMessageOverrides() []uint64 {
	return p.CustomMessage
}

// CustomFeatureBits returns the set of protocol feature bits that should be
// advertised.
func (p ExperimentalProtocol) CustomFeatureBits() []lnwire.FeatureBit {
	features := make([]lnwire.FeatureBit, len(p.CustomFeature))

	for i, feature := range p.CustomFeature {
		features[i] = lnwire.FeatureBit(feature)
	}

	return features
}
