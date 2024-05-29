package quiescence

import (
	"github.com/lightningnetwork/lnd/fn"
	"github.com/lightningnetwork/lnd/lnwire"
)

type QuiescenceMgr interface {
	RecvStfu(msg lnwire.Stfu) error
	InitStfu(resp chan<- fn.Option[bool]) error
	TryProgressState() error
	CanSendUpdates() bool
	CanRecvUpdates() bool
	RegisterHook(func ())
}
