//go:build dev
// +build dev

package lncfg

// ExperimentalProtocol is a sub-config that houses any experimental protocol
// features that also require a build-tag to activate.
type ExperimentalProtocol struct {
	CustomMessage []uint64 `long:"custom-message" description:"allows the custom message apis to send and report messages with the protocol number provided that fall outside of the custom message number range."`
}

// CustomMessageOverrides returns the set of protocol messages that we override
// to allow custom handling.
func (p ExperimentalProtocol) CustomMessageOverrides() []uint64 {
	return p.CustomMessage
}
