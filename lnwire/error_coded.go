package lnwire

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/tlv"
)

// ErrorCode is an enum that defines the different types of error codes that
// are used to enrich the meaning of errors.
type ErrorCode uint16

const (
	// MaxPendingChannels indicates that the number of active pending
	// channels exceeds their maximum policy limit.
	MaxPendingChannels ErrorCode = 1

	// SynchronizingChain indicates that the peer is still busy syncing
	// the latest state of the blockchain.
	SynchronizingChain ErrorCode = 3

	// MaxPendingHtlcsExceeded indicates that the remote peer has tried to
	// add more htlcs that our local policy allows to a commitment.
	MaxPendingHtlcsExceeded ErrorCode = 5

	// MaxPendingAmountExceeded indicates that the remote peer has tried to
	// add more than our pending amount in flight local policy limit to a
	// commitment.
	MaxPendingAmountExceeded ErrorCode = 7

	// ErrInternalError indicates that something internal has failed, and
	// we do not want to provide our peer with further information.
	ErrInternalError ErrorCode = 9

	// ErrRemoteError indicates that our peer sent an error, prompting us
	// to fail the connection.
	ErrRemoteError ErrorCode = 11

	// ErrSyncError indicates that we failed synchronizing the state of the
	// channel with our peer.
	ErrSyncError ErrorCode = 13

	// ErrRecoveryError the channel was unable to be resumed, we need the
	// remote party to force close the channel out on chain now as a
	// result.
	ErrRecoveryError ErrorCode = 15

	// ErrInvalidUpdate indicates that the peer send us an invalid update.
	ErrInvalidUpdate ErrorCode = 17

	// ErrInvalidRevocation indicates that the remote peer send us an
	// invalid revocation message.
	ErrInvalidRevocation ErrorCode = 19

	// ErrInvalidCommitSig indicates that we have received an invalid
	// commitment signature.
	ErrInvalidCommitSig ErrorCode = 21

	// ErrInvalidHtlcSig indicates that we have received an invalid htlc
	// signature.
	ErrInvalidHtlcSig ErrorCode = 23
)

// Compile time assertion that CodedError implements the ExtendedError
// interface.
var _ ExtendedError = (*CodedError)(nil)

// CodedError is an error that has been enriched with an error code, and
// optional additional information.
type CodedError struct {
	// ErrorCode is the error code that defines the type of error this is.
	ErrorCode

	// ErrContext contains additional information used to enrich the error.
	ErrContext
}

// NewCodedError creates an error with the code provided.
func NewCodedError(e ErrorCode) *CodedError {
	return &CodedError{
		ErrorCode: e,
	}
}

// Error provides a string representation of a coded error.
func (e *CodedError) Error() string {
	var errStr string

	switch e.ErrorCode {
	case MaxPendingChannels:
		errStr = "number of pending channels exceed maximum"

	case SynchronizingChain:
		errStr = "synchronizing blockchain"

	case MaxPendingHtlcsExceeded:
		errStr = "commitment exceeds max htlcs"

	case MaxPendingAmountExceeded:
		errStr = "commitment exceeds max in flight value"

	case ErrInternalError:
		errStr = "internal error"

	case ErrRemoteError:
		errStr = "remote error"

	case ErrSyncError:
		errStr = "sync error"

	case ErrRecoveryError:
		errStr = "unable to resume channel, recovery required"

	case ErrInvalidUpdate:
		errStr = "invalid update"

	case ErrInvalidRevocation:
		errStr = "invalid revocation"

	// TODO(carla): better error string here using other info?
	case ErrInvalidCommitSig:
		errStr = "invalid commit sig"

	case ErrInvalidHtlcSig:
		errStr = "invalid htlc sig"

	default:
		errStr = "unknown"
	}

	return fmt.Sprintf("Error code: %d: %v", e.ErrorCode, errStr)
}

// ErrContext is an interface implemented by coded errors with additional
// information about their error code.
type ErrContext interface {
	// Records returns a set of TLVs describing additional information
	// added to an error code.
	Records() []tlv.Record
}

// knownErrorCodeContext maps known error codes to additional information that
// is included in tlvs.
var knownErrorCodeContext = map[ErrorCode]ErrContext{
	ErrInvalidCommitSig: &InvalidCommitSigError{},
	ErrInvalidHtlcSig:   &InvalidHtlcSigError{},
}

// Record provides a tlv record for coded errors.
func (e *CodedError) Record() tlv.Record {
	return tlv.MakeDynamicRecord(
		typeErrorCode, e, e.sizeFunc, codedErrorEncoder,
		codedErrorDecoder,
	)
}

func (e *CodedError) sizeFunc() uint64 {
	var (
		b   bytes.Buffer
		buf [8]byte
	)

	// TODO(carla): copied, maybe another way here, log error?
	if err := codedErrorEncoder(&b, e, &buf); err != nil {
		panic(fmt.Sprintf("coded error encoder failed: %v", err))
	}

	return uint64(len(b.Bytes()))
}

func codedErrorEncoder(w io.Writer, val interface{}, buf *[8]byte) error {
	v, ok := val.(*CodedError)
	if ok {
		code := uint16(v.ErrorCode)
		if err := tlv.EUint16(w, &code, buf); err != nil {
			return err
		}

		// If we have extra records present, we want to store them.
		// Even if records is empty, we continue with the nested record
		// encoding so that a 0 value length will be written.
		var records []tlv.Record
		if v.ErrContext != nil {
			records = v.Records()
		}

		// Create a tlv stream containing the nested tlvs.
		tlvStream, err := tlv.NewStream(records...)
		if err != nil {
			return err
		}

		// Encode the nested tlvs is a _separate_ buffer so that we
		// can get the length of our nested stream.
		var nestedBuffer bytes.Buffer
		if err := tlvStream.Encode(&nestedBuffer); err != nil {
			return err
		}

		// Write the length of the nested tlv stream to the main
		// buffer, followed by the actual values.
		nestedBytes := uint64(nestedBuffer.Len())
		if err := tlv.WriteVarInt(w, nestedBytes, buf); err != nil {
			return err
		}

		if _, err := w.Write(nestedBuffer.Bytes()); err != nil {
			return err
		}

		return nil
	}

	return tlv.NewTypeForEncodingErr(val, "lnwire.CodedError")
}

func codedErrorDecoder(r io.Reader, val interface{}, buf *[8]byte,
	_ uint64) error {

	v, ok := val.(*CodedError)
	if ok {
		var errCode uint16
		if err := tlv.DUint16(r, &errCode, buf, 2); err != nil {
			return err
		}

		errorCode := ErrorCode(errCode)
		*v = CodedError{
			ErrorCode: errorCode,
		}

		nestedLen, err := tlv.ReadVarInt(r, buf)
		if err != nil {
			return err
		}

		// If there are no nested fields, we don't need to ready any
		// further values.
		if nestedLen == 0 {
			return nil
		}

		// Using this information, we'll create a new limited
		// reader that'll return an EOF once the end has been
		// reached so the stream stops consuming bytes.
		//
		// TODO(carla): copied from #5803
		innerTlvReader := io.LimitedReader{
			R: r,
			N: int64(nestedLen),
		}

		// Lookup the records for this error code. If we don't know of
		// any additional records that are nested for this error code,
		// that's ok, we just don't read them (allowing forwards
		// compatibility for new fields).
		errContext, known := knownErrorCodeContext[errorCode]
		if !known {
			return nil
		}

		tlvStream, err := tlv.NewStream(errContext.Records()...)
		if err != nil {
			return err
		}

		if err := tlvStream.Decode(&innerTlvReader); err != nil {
			return err
		}

		*v = CodedError{
			ErrorCode:  errorCode,
			ErrContext: errContext,
		}

		return nil
	}

	return tlv.NewTypeForEncodingErr(val, "lnwire.CodedError")
}

// InvalidCommitSigError contains the error information we transmit upon
// receiving an invalid commit signature
type InvalidCommitSigError struct {
	commitHeight uint64
	commitSig    []byte
	sigHash      []byte
	commitTx     []byte
}

// A compile time flag to ensure that InvalidCommitSigError implements the
// ErrContext interface.
var _ ErrContext = (*InvalidCommitSigError)(nil)

// NewInvalidCommitSigError creates an invalid sig error.
func NewInvalidCommitSigError(commitHeight uint64, commitSig, sigHash,
	commitTx []byte) *CodedError {

	return &CodedError{
		ErrorCode: ErrInvalidCommitSig,
		ErrContext: &InvalidCommitSigError{
			commitHeight: commitHeight,
			commitSig:    commitSig,
			sigHash:      sigHash,
			commitTx:     commitTx,
		},
	}
}

// Records returns a set of record producers for the tlvs associated
// with an enriched error.
func (i *InvalidCommitSigError) Records() []tlv.Record {
	return []tlv.Record{
		tlv.MakePrimitiveRecord(typeNestedCommitHeight, &i.commitHeight),
		tlv.MakePrimitiveRecord(typeNestedCommitSig, &i.commitSig),
		tlv.MakePrimitiveRecord(typeNestedSigHash, &i.sigHash),
		tlv.MakePrimitiveRecord(typeNestedCommitTx, &i.commitTx),
	}
}

// InvalidHtlcSigError is a struct that implements the error interface to
// report a failure to validate an htlc signature from a remote peer. We'll use
// the items in this struct to generate a rich error message for the remote
// peer when we receive an invalid signature from it. Doing so can greatly aide
// in debugging across implementation issues.
type InvalidHtlcSigError struct {
	commitHeight uint64
	htlcSig      []byte
	htlcIndex    uint64
	sigHash      []byte
	commitTx     []byte
}

// A compile time flag to ensure that InvalidHtlcSigError implements the
// ErrContext interface.
var _ ErrContext = (*InvalidHtlcSigError)(nil)

// NewInvalidHtlcSigError creates an invalid htlc signature error.
func NewInvalidHtlcSigError(commitHeight, htlcIndex uint64, htlcSig, sigHash,
	commitTx []byte) *CodedError {

	return &CodedError{
		ErrorCode: ErrInvalidHtlcSig,
		ErrContext: &InvalidHtlcSigError{
			commitHeight: commitHeight,
			htlcIndex:    htlcIndex,
			htlcSig:      htlcSig,
			sigHash:      sigHash,
			commitTx:     commitTx,
		},
	}
}

// Records returns a set of record producers for the tlvs associated with
// an enriched error.
func (i *InvalidHtlcSigError) Records() []tlv.Record {
	return []tlv.Record{
		tlv.MakePrimitiveRecord(typeNestedCommitHeight, &i.commitHeight),
		tlv.MakePrimitiveRecord(typeNestedHtlcIndex, &i.htlcIndex),
		tlv.MakePrimitiveRecord(typeNestedHtlcSig, &i.htlcSig),
		tlv.MakePrimitiveRecord(typeNestedSigHash, &i.sigHash),
		tlv.MakePrimitiveRecord(typeNestedCommitTx, &i.commitTx),
	}
}
