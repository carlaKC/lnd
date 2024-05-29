package quiescence

import (
	"fmt"

	"github.com/lightningnetwork/lnd/fn"
	"github.com/lightningnetwork/lnd/lnwire"
)

// Quiescer is a state machine that tracks progression through the quiescence
// protocol.
type Quiescer struct {
	// chanID marks what channel we are managing the state machine for. This
	// is important because the quiescer is responsible for constructing the
	// messages we send out and the ChannelID is a key field in that
	// message.
	chanID lnwire.ChannelID

	// weOpened indicates whether we were the original opener of the
	// channel. This is used to break ties when both sides of the channel
	// send Stfu claiming to be the initiator.
	weOpened bool

	// localInit indicates whether our path through this state machine was
	// initiated by our node. This can be true or false independently of
	// remoteInit.
	localInit bool

	// remoteInit indicates whether we received Stfu from our peer where the
	// message indicated that the remote node believes it was the initiator.
	// This can be true or false independently of localInit.
	remoteInit bool

	// sent tracks whether or not we have emitted Stfu for sending.
	sent bool

	// received tracks whether or not we have received Stfu from our peer.
	received bool

	// resp is the channel that we send a signal on when we have achieved
	// quiescence, optionally nil if we are not the initiator.
	resp chan<- fn.Option[bool]

	// sendStfuMsg is responsible for sending the stfu message to our peer.
	sendStfuMsg func(stfu lnwire.Stfu) error

	// pendingState returns true if there are no updates pending on the
	// local or remote commitment.
	pendingState func() bool

	// resumeQueue
	resumeQueue []func()
}

// NewQuiescer returns a new quiescence state machine that handles the
// quiescence protocol using the closures provided to obtain state information
// from external systems.
func NewQuiescer(chanId lnwire.ChannelID, weOpened bool,
	sendStfuMsg func(lnwire.Stfu) error,
	pendingState func() bool) *Quiescer {

	return &Quiescer{
		chanID:       chanId,
		weOpened:     weOpened,
		sendStfuMsg:  sendStfuMsg,
		pendingState: pendingState,
	}
}

// recvStfu is called when we receive an Stfu message from the remote.
func (q *Quiescer) RecvStfu(msg lnwire.Stfu) error {
	if q.received {
		return fmt.Errorf("stfu already received for channel %v",
			q.chanID)
	}

	q.received = true
	q.remoteInit = msg.Initiator

	// TODO(carla): check on ordering here!
	q.tryResolveQuiescenceRequests()

	// If we can immediately send an Stfu response back, we will.
	return q.TryProgressState()
}

// sendStfu is called when we are ready to send an Stfu message. It returns the
// Stfu message to be sent.
func (q *Quiescer) sendStfu() (fn.Option[lnwire.Stfu], error) {
	if q.sent {
		return fn.None[lnwire.Stfu](),
			fmt.Errorf(
				"stfu arleady sent for channel %s",
				q.chanID.String(),
			)
	}

	stfu := lnwire.Stfu{
		ChanID:    q.chanID,
		Initiator: q.localInit,
	}

	q.sent = true

	return fn.Some(stfu), nil
}

// oweStfu returns true if we owe the other party an Stfu. We owe the remote an
// Stfu when we have received but not yet sent an Stfu.
func (q *Quiescer) oweStfu() bool {
	return (q.received || q.localInit) && !q.sent
}

// needStfu returns true if the remote owes us an Stfu. They owe us an Stfu when
// we have sent but not yet received an Stfu.
func (q *Quiescer) needStfu() bool {
	return q.sent && !q.received
}

// isQuiescent returns true if the state machine has been driven all the way to
// completion. If this returns true, processes that depend on channel quiescence
// may proceed.
func (q *Quiescer) isQuiescent() bool {
	return q.sent && q.received
}

// isLocallyInitiatedFinal determines whether we are the initiator of quiescence
// for the purposes of downstream protocols.
func (q *Quiescer) isLocallyInitiatedFinal() fn.Option[bool] {
	if !q.isQuiescent() {
		return fn.None[bool]()
	}

	// We assume it is impossible for both to be false, if the channel is
	// quiescent. However, we use the same tie-breaking scheme no matter
	// what.
	if q.localInit == q.remoteInit {
		return fn.Some(q.weOpened)
	}

	// In this case we know that only one of the values is set so we just
	// return the one that indicates whether we end up as the initiator.
	return fn.Some(q.localInit)
}

// CanSendUpdates returns true if we haven't yet sent an Stfu which would mark
// the end of our ability to send updates.
func (q *Quiescer) CanSendUpdates() bool {
	return !q.sent && !q.localInit
}

// canRecvUpdates returns true if we haven't yet received an Stfu which would
// mark the end of the remote's ability to send updates.
func (q *Quiescer) CanRecvUpdates() bool {
	return !q.received
}

// initStfu instructs the quiescer that we intend to begin a quiescence
// negotiation where we are the initiator. We don't yet send stfu yet because
// we need to wait for the link to give us a valid opportunity to do so.
func (q *Quiescer) InitStfu(resp chan<- fn.Option[bool]) error {
	if q.localInit {
		close(resp)
		return fmt.Errorf("quiescence already requested")
	}

	q.localInit = true
	q.resp = resp

	// Now that we've initiated the quiescence, try to move our state
	// forward if appropriate.
	return q.TryProgressState()
}

func (q *Quiescer) TryProgressState() error {
	if !q.oweStfu() {
		return nil
	}

	// If we have any updates pending, we can't progress further.
	if q.pendingState() {
		return nil
	}

	// If we can enter quiescence, get the message to be sent (if any) and
	// sent it to our peer.
	oStfu, err := q.sendStfu()
	if err != nil {
		return err
	}

	oStfu.WhenSome(func(stfu lnwire.Stfu) {
		err = q.sendStfuMsg(stfu)
		if err != nil {
			return
		}

		// Once we've notified our peer, send any notifications
		// required.
		q.tryResolveQuiescenceRequests()
	})

	return err
}

func (q *Quiescer) tryResolveQuiescenceRequests() {
	if q.isQuiescent() {
		return
	}

	// If no response channel is registered, we don't need to notify anyone.
	if q.resp == nil {
		return
	}

	ourTurn := q.isLocallyInitiatedFinal()
	ourTurn.WhenSome(func(ourTurn bool) {
		// TODO: expect channel to be buffered or select on quit.
		q.resp <- fn.Some(ourTurn)
	})
}

// onResume accepts a no return closure that will run when the quiescer is
// resumed.
// TODO(carla): if we always exit with disconnection why do we need this?
// - Possibly because we have another signal in downstream to un-quiesce?
func (q *Quiescer) RegisterHook(hook func()) {
	q.resumeQueue = append(q.resumeQueue, hook)
}

// resume runs all of the deferred actions that have accumulated while the
// channel has been quiescent and then resets the quiescer state to its initial
// state.
func (q *Quiescer) resume() {
	for _, hook := range q.resumeQueue {
		hook()
	}
	q.localInit = false
	q.remoteInit = false
	q.sent = false
	q.received = false
	q.resumeQueue = nil
}
