package lnwallet

import (
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ErrZeroCapacity returns an error indicating the funder attempted to put zero
// funds into the channel.
func ErrZeroCapacity() *lnwire.StructuredError {
	return lnwire.NewStructuredError(lnwire.MsgOpenChannel, 2, nil, 0)
}

// ErrChainMismatch returns an error indicating that the initiator tried to
// open a channel for an unknown chain.
func ErrChainMismatch(knownChain,
	unknownChain *chainhash.Hash) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 0, knownChain, unknownChain,
	)
}

// ErrFunderBalanceDust returns an error indicating the initial balance of the
// funder is considered dust at the current commitment fee.
func ErrFunderBalanceDust(commitFee, funderBalance,
	minBalance uint64) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 2, minBalance, funderBalance,
	)
}

// ErrCsvDelayTooLarge returns an error indicating that the CSV delay was to
// large to be accepted, along with the current max.
func ErrCsvDelayTooLarge(remoteDelay,
	maxDelay uint16) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 9, maxDelay, remoteDelay,
	)
}

// ErrChanReserveTooSmall returns an error indicating that the channel reserve
// the remote is requiring is too small to be accepted.
func ErrChanReserveTooSmall(reserve,
	dustLimit btcutil.Amount) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 6, uint64(dustLimit), uint64(reserve),
	)
}

// ErrChanReserveTooLarge returns an error indicating that the chan reserve the
// remote is requiring, is too large to be accepted.
func ErrChanReserveTooLarge(reserve,
	maxReserve btcutil.Amount) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 6, uint64(maxReserve), uint64(reserve),
	)
}

// ErrNonZeroPushAmount is returned by a remote peer that receives a
// FundingOpen request for a channel with non-zero push amount while
// they have 'rejectpush' enabled.
func ErrNonZeroPushAmount(amt uint64) *lnwire.StructuredError {
	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 3, uint64(0), amt,
	)
}

// ErrMinHtlcTooLarge returns an error indicating that the MinHTLC value the
// remote required is too large to be accepted.
func ErrMinHtlcTooLarge(minHtlc,
	maxMinHtlc lnwire.MilliSatoshi) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 7, uint64(maxMinHtlc), uint64(minHtlc),
	)
}

// ErrMaxHtlcNumTooLarge returns an error indicating that the 'max HTLCs in
// flight' value the remote required is too large to be accepted.
func ErrMaxHtlcNumTooLarge(maxHtlc, maxMaxHtlc uint16) *lnwire.StructuredError {
	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 10, maxMaxHtlc, maxMaxHtlc,
	)
}

// ErrMaxHtlcNumTooSmall returns an error indicating that the 'max HTLCs in
// flight' value the remote required is too small to be accepted.
func ErrMaxHtlcNumTooSmall(maxHtlc, minMaxHtlc uint16) *lnwire.StructuredError {
	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 10, minMaxHtlc, maxHtlc,
	)
}

// ErrMaxValueInFlightTooSmall returns an error indicating that the 'max HTLC
// value in flight' the remote required is too small to be accepted.
func ErrMaxValueInFlightTooSmall(maxValInFlight,
	minMaxValInFlight lnwire.MilliSatoshi) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 5, minMaxValInFlight, maxValInFlight,
	)
}

// ErrNumConfsTooLarge returns an error indicating that the number of
// confirmations required for a channel is too large.
func ErrNumConfsTooLarge(numConfs, maxNumConfs uint32) *lnwire.StructuredError {
	return lnwire.NewStructuredError(
		lnwire.MsgAcceptChannel, 5, maxNumConfs, numConfs,
	)
}

// ErrChanTooSmall returns an error indicating that an incoming channel request
// was too small. We'll reject any incoming channels if they're below our
// configured value for the min channel size we'll accept.
func ErrChanTooSmall(chanSize,
	minChanSize btcutil.Amount) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 2, uint64(minChanSize), uint64(chanSize),
	)
}

// ErrChanTooLarge returns an error indicating that an incoming channel request
// was too large. We'll reject any incoming channels if they're above our
// configured value for the max channel size we'll accept.
func ErrChanTooLarge(chanSize,
	maxChanSize btcutil.Amount) *lnwire.StructuredError {

	return lnwire.NewStructuredError(
		lnwire.MsgOpenChannel, 2, uint64(maxChanSize), uint64(chanSize),
	)
}

// ErrHtlcIndexAlreadyFailed is returned when the HTLC index has already been
// failed, but has not been committed by our commitment state.
type ErrHtlcIndexAlreadyFailed uint64

// Error returns a message indicating the index that had already been failed.
func (e ErrHtlcIndexAlreadyFailed) Error() string {
	return fmt.Sprintf("HTLC with ID %d has already been failed", e)
}

// ErrHtlcIndexAlreadySettled is returned when the HTLC index has already been
// settled, but has not been committed by our commitment state.
type ErrHtlcIndexAlreadySettled uint64

// Error returns a message indicating the index that had already been settled.
func (e ErrHtlcIndexAlreadySettled) Error() string {
	return fmt.Sprintf("HTLC with ID %d has already been settled", e)
}

// ErrInvalidSettlePreimage is returned when trying to settle an HTLC, but the
// preimage does not correspond to the payment hash.
type ErrInvalidSettlePreimage struct {
	preimage []byte
	rhash    []byte
}

// Error returns an error message with the offending preimage and intended
// payment hash.
func (e ErrInvalidSettlePreimage) Error() string {
	return fmt.Sprintf("Invalid payment preimage %x for hash %x",
		e.preimage, e.rhash)
}

// ErrUnknownHtlcIndex is returned when locally settling or failing an HTLC, but
// the HTLC index is not known to the channel. This typically indicates that the
// HTLC was already settled in a prior commitment.
type ErrUnknownHtlcIndex struct {
	chanID lnwire.ShortChannelID
	index  uint64
}

// Error returns an error logging the channel and HTLC index that was unknown.
func (e ErrUnknownHtlcIndex) Error() string {
	return fmt.Sprintf("No HTLC with ID %d in channel %v",
		e.index, e.chanID)
}
