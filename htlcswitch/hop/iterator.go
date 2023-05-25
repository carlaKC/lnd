package hop

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	sphinx "github.com/lightningnetwork/lightning-onion"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/record"
)

// Iterator is an interface that abstracts away the routing information
// included in HTLC's which includes the entirety of the payment path of an
// HTLC. This interface provides two basic method which carry out: how to
// interpret the forwarding information encoded within the HTLC packet, and hop
// to encode the forwarding information for the _next_ hop.
type Iterator interface {
	// HopPayload returns the set of fields that detail exactly _how_ this
	// hop should forward the HTLC to the next hop.  Additionally, the
	// information encoded within the returned ForwardingInfo is to be used
	// by each hop to authenticate the information given to it by the prior
	// hop. The payload will also contain any additional TLV fields provided
	// by the sender.
	HopPayload() (*Payload, error)

	// EncodeNextHop encodes the onion packet destined for the next hop
	// into the passed io.Writer.
	EncodeNextHop(w io.Writer) error

	// ExtractErrorEncrypter returns the ErrorEncrypter needed for this hop,
	// along with a failure code to signal if the decoding was successful.
	ExtractErrorEncrypter(ErrorEncrypterExtracter) (ErrorEncrypter,
		lnwire.FailCode)
}

// sphinxHopIterator is the Sphinx implementation of hop iterator which uses
// onion routing to encode the payment route  in such a way so that node might
// see only the next hop in the route..
type sphinxHopIterator struct {
	// ogPacket is the original packet from which the processed packet is
	// derived.
	ogPacket *sphinx.OnionPacket

	// processedPacket is the outcome of processing an onion packet. It
	// includes the information required to properly forward the packet to
	// the next hop.
	processedPacket *sphinx.ProcessedPacket

	// blindingKit contains the elements required to process hops that are
	// part of a blinded route.
	blindingKit *BlindingKit
}

// makeSphinxHopIterator converts a processed packet returned from a sphinx
// router and converts it into an hop iterator for usage in the link. A
// blinding kit is passed through for the link to obtain forwarding information
// for blinded routes.
func makeSphinxHopIterator(ogPacket *sphinx.OnionPacket,
	packet *sphinx.ProcessedPacket,
	blindingKit *BlindingKit) *sphinxHopIterator {

	return &sphinxHopIterator{
		ogPacket:        ogPacket,
		processedPacket: packet,
		blindingKit:     blindingKit,
	}
}

// A compile time check to ensure sphinxHopIterator implements the HopIterator
// interface.
var _ Iterator = (*sphinxHopIterator)(nil)

// Encode encodes iterator and writes it to the writer.
//
// NOTE: Part of the HopIterator interface.
func (r *sphinxHopIterator) EncodeNextHop(w io.Writer) error {
	return r.processedPacket.NextPacket.Encode(w)
}

// HopPayload returns the set of fields that detail exactly _how_ this hop
// should forward the HTLC to the next hop.  Additionally, the information
// encoded within the returned ForwardingInfo is to be used by each hop to
// authenticate the information given to it by the prior hop. The payload will
// also contain any additional TLV fields provided by the sender.
//
// NOTE: Part of the HopIterator interface.
func (r *sphinxHopIterator) HopPayload() (*Payload, error) {
	switch r.processedPacket.Payload.Type {

	// If this is the legacy payload, then we'll extract the information
	// directly from the pre-populated ForwardingInstructions field.
	case sphinx.PayloadLegacy:
		fwdInst := r.processedPacket.ForwardingInstructions
		return NewLegacyPayload(fwdInst), nil

	// Otherwise, if this is the TLV payload, then we'll make a new stream
	// to decode only what we need to make routing decisions.
	case sphinx.PayloadTLV:
		return NewPayloadFromReader(bytes.NewReader(
			r.processedPacket.Payload.Payload,
		), r.blindingKit)

	default:
		return nil, fmt.Errorf("unknown sphinx payload type: %v",
			r.processedPacket.Payload.Type)
	}
}

// ExtractErrorEncrypter decodes and returns the ErrorEncrypter for this hop,
// along with a failure code to signal if the decoding was successful. The
// ErrorEncrypter is used to encrypt errors back to the sender in the event that
// a payment fails.
//
// NOTE: Part of the HopIterator interface.
func (r *sphinxHopIterator) ExtractErrorEncrypter(
	extracter ErrorEncrypterExtracter) (ErrorEncrypter, lnwire.FailCode) {

	return extracter(r.ogPacket.EphemeralKey)
}

// BlindingProcessor is an interface that provides the cryptographic operations
// required for processing blinded hops.
type BlindingProcessor interface {
	// DecryptBlindedData decrypts a blinded blob of data using the
	// ephemeral key provided.
	DecryptBlindedData(*btcec.PublicKey, []byte) ([]byte, error)

	// NextEphemeral returns the next hop's ephemeral key, calculated
	// from the current ephemeral key provided.
	NextEphemeral(*btcec.PublicKey) (*btcec.PublicKey, error)
}

// BlindingKit contains the components required to extract forwarding
// information for hops in a blinded route.
type BlindingKit struct {
	// BlindingPoint holds a blinding point that was passed to the node via
	// update_add_htlc's TLVs.
	BlindingPoint *btcec.PublicKey

	// lastHop indicates whether we're in the last hop in the onion route.
	lastHop bool

	// forwardingInfo uses the ephemeral blinding key provided to decrypt
	// a blob of encrypted data provided in the onion and obtain the
	// forwarding information for the blinded hop.
	forwardingInfo func(*btcec.PublicKey, []byte) (*ForwardingInfo,
		error)
}

// MakeBlindingKit produces a kit that is used to decrypte and decode
// forwarding information for hops in blinded routes.
func MakeBlindingKit(processor BlindingProcessor,
	blindingPoint *btcec.PublicKey, lastHop bool,
	incomingAmount lnwire.MilliSatoshi, incomingCltv uint32) *BlindingKit {

	return &BlindingKit{
		BlindingPoint: blindingPoint,
		lastHop:       lastHop,
		forwardingInfo: deriveForwardingInfo(
			processor, incomingAmount, incomingCltv,
		),
	}
}

// deriveForwardingInfo produces a function that will decrypt and deserialize
// an encrypted blob of data for a hop in a blinded route and reconstruct the
// forwarding information for the hop from the information provided.
func deriveForwardingInfo(processor BlindingProcessor,
	incomingAmount lnwire.MilliSatoshi, incomingCltv uint32) func(
	*btcec.PublicKey, []byte) (*ForwardingInfo, error) {

	return func(blinding *btcec.PublicKey, data []byte) (*ForwardingInfo,
		error) {

		decrypted, err := processor.DecryptBlindedData(blinding, data)
		if err != nil {
			return nil, fmt.Errorf("decrypt blinded data: %w", err)
		}

		b := bytes.NewBuffer(decrypted)
		routeData, err := record.DecodeBlindedRouteData(b)
		if err != nil {
			return nil, fmt.Errorf("decode route data: %w", err)
		}

		if err := validateBlindedRouteData(
			routeData, incomingAmount, incomingCltv,
		); err != nil {
			return nil, err
		}
		// If we have our short channel ID or expiry present, set
		// values in our forwarding information. We start with the
		// incoming values as defaults so that they will have the
		// correct values for the final hop in the blinded route
		// (which does not have relay info set).
		var (
			nextHop = Exit
			expiry  = incomingCltv
			fwdAmt  = incomingAmount
		)

		if routeData.ShortChannelID != nil {
			nextHop = *routeData.ShortChannelID
		}

		if routeData.RelayInfo != nil {
			fwdAmt, err = calculateForwardingAmount(
				incomingAmount, routeData.RelayInfo.BaseFee,
				routeData.RelayInfo.FeeRate,
			)
			if err != nil {
				return nil, err
			}

			expiry = incomingCltv - uint32(
				routeData.RelayInfo.CltvExpiryDelta,
			)
		}

		nextEph, err := processor.NextEphemeral(blinding)
		if err != nil {
			return nil, err
		}

		return &ForwardingInfo{
			Network:         BitcoinNetwork,
			NextHop:         nextHop,
			AmountToForward: fwdAmt,
			OutgoingCTLV:    expiry,
			NextBlinding:    nextEph,
		}, nil
	}
}

// calculateForwardingAmount calculates the amount to forward for a blinded
// hop based on the incoming amount and forwarding parameters.
//
// When forwarding a payment, the fee we take is calculated, not on the
// incoming amount, but rather on the amount we forward. We charge fees based
// on our own liquidity we are forwarding downstream.
//
// With route blinding, we are NOT given the amount to forward.  This
// unintuitive looking formula comes from the fact that without the amount to
// forward, we cannot compute the fees taken directly.
//
// The amount to be forwarded can be computed as follows:
//
//	amt_to_forward = incoming_amount - total_fees //nolint:dupword
//	total_fees = base_fee + amt_to_forward*(fee_rate/1000000)
//
// After substitution and some massaging you will get:
//
//		amt_to_forward = (incoming_amount - base_fee) /
//	                      ( 1 + fee_rate / 1000000 )
//
// From there we use a ceiling formula for integer division so that we always
// round up, otherwise the sender may receive slightly less than intended:
//
//	ceil(a/b) = (a + b - 1)/(b)
func calculateForwardingAmount(incomingAmount lnwire.MilliSatoshi, baseFee,
	proportionalFee uint32) (lnwire.MilliSatoshi, error) {

	// proportionalParts is the number of parts that our proportional fee
	// is expressed per.
	var proportionalParts uint64 = 1_000_000

	// Sanity check to prevent overflow.
	if incomingAmount < lnwire.MilliSatoshi(baseFee) {
		return 0, fmt.Errorf("incoming amount: %v < base fee: %v",
			incomingAmount, baseFee)
	}

	ceiling := ((uint64(incomingAmount) - uint64(baseFee)) +
		(1 + uint64(proportionalFee)/proportionalParts) - 1) /
		(1 + uint64(proportionalFee)/proportionalParts)

	return lnwire.MilliSatoshi(ceiling), nil
}

// OnionProcessor is responsible for keeping all sphinx dependent parts inside
// and expose only decoding function. With such approach we give freedom for
// subsystems which wants to decode sphinx path to not be dependable from
// sphinx at all.
//
// NOTE: The reason for keeping decoder separated from hop iterator is too
// maintain the hop iterator abstraction. Without it the structures which using
// the hop iterator should contain sphinx router which makes their creations in
// tests dependent from the sphinx internal parts.
type OnionProcessor struct {
	router *sphinx.Router
}

// NewOnionProcessor creates new instance of decoder.
func NewOnionProcessor(router *sphinx.Router) *OnionProcessor {
	return &OnionProcessor{router}
}

// Start spins up the onion processor's sphinx router.
func (p *OnionProcessor) Start() error {
	log.Info("Onion processor starting")
	return p.router.Start()
}

// Stop shutsdown the onion processor's sphinx router.
func (p *OnionProcessor) Stop() error {

	log.Info("Onion processor shutting down")

	p.router.Stop()
	return nil
}

// ReconstructHopIterator attempts to decode a valid sphinx packet from the
// passed io.Reader instance using the rHash as the associated data when
// checking the relevant MACs during the decoding process.
func (p *OnionProcessor) ReconstructHopIterator(r io.Reader, rHash []byte) (
	Iterator, error) {

	onionPkt := &sphinx.OnionPacket{}
	if err := onionPkt.Decode(r); err != nil {
		return nil, err
	}

	// Attempt to process the Sphinx packet. We include the payment hash of
	// the HTLC as it's authenticated within the Sphinx packet itself as
	// associated data in order to thwart attempts a replay attacks. In the
	// case of a replay, an attacker is *forced* to use the same payment
	// hash twice, thereby losing their money entirely.
	//
	// TODO(carla): contract court will need to be able to pass the
	// blinding point back in here (requires interface update).
	sphinxPacket, err := p.router.ReconstructOnionPacket(
		onionPkt, rHash, nil,
	)
	if err != nil {
		return nil, err
	}

	return makeSphinxHopIterator(onionPkt, sphinxPacket, nil), nil
}

// DecodeHopIteratorRequest encapsulates all date necessary to process an onion
// packet, perform sphinx replay detection, and schedule the entry for garbage
// collection.
type DecodeHopIteratorRequest struct {
	OnionReader    io.Reader
	RHash          []byte
	IncomingCltv   uint32
	IncomingAmount lnwire.MilliSatoshi
	BlindingPoint  *btcec.PublicKey
}

// DecodeHopIteratorResponse encapsulates the outcome of a batched sphinx onion
// processing.
type DecodeHopIteratorResponse struct {
	HopIterator Iterator
	FailCode    lnwire.FailCode
}

// Result returns the (HopIterator, lnwire.FailCode) tuple, which should
// correspond to the index of a particular DecodeHopIteratorRequest.
//
// NOTE: The HopIterator should be considered invalid if the fail code is
// anything but lnwire.CodeNone.
func (r *DecodeHopIteratorResponse) Result() (Iterator, lnwire.FailCode) {
	return r.HopIterator, r.FailCode
}

// DecodeHopIterators performs batched decoding and validation of incoming
// sphinx packets. For the same `id`, this method will return the same iterators
// and failcodes upon subsequent invocations.
//
// NOTE: In order for the responses to be valid, the caller must guarantee that
// the presented readers and rhashes *NEVER* deviate across invocations for the
// same id.
func (p *OnionProcessor) DecodeHopIterators(id []byte,
	reqs []DecodeHopIteratorRequest) ([]DecodeHopIteratorResponse, error) {

	var (
		batchSize = len(reqs)
		onionPkts = make([]sphinx.OnionPacket, batchSize)
		resps     = make([]DecodeHopIteratorResponse, batchSize)
	)

	tx := p.router.BeginTxn(id, batchSize)

	decode := func(seqNum uint16, onionPkt *sphinx.OnionPacket,
		req DecodeHopIteratorRequest) lnwire.FailCode {

		err := onionPkt.Decode(req.OnionReader)
		switch err {
		case nil:
			// success

		case sphinx.ErrInvalidOnionVersion:
			return lnwire.CodeInvalidOnionVersion

		case sphinx.ErrInvalidOnionKey:
			return lnwire.CodeInvalidOnionKey

		default:
			log.Errorf("unable to decode onion packet: %v", err)
			return lnwire.CodeInvalidOnionKey
		}

		err = tx.ProcessOnionPacket(
			seqNum, onionPkt, req.RHash, req.IncomingCltv,
			req.BlindingPoint,
		)
		switch err {
		case nil:
			// success
			return lnwire.CodeNone

		case sphinx.ErrInvalidOnionVersion:
			return lnwire.CodeInvalidOnionVersion

		case sphinx.ErrInvalidOnionHMAC:
			return lnwire.CodeInvalidOnionHmac

		case sphinx.ErrInvalidOnionKey:
			return lnwire.CodeInvalidOnionKey

		default:
			log.Errorf("unable to process onion packet: %v", err)
			return lnwire.CodeInvalidOnionKey
		}
	}

	// Execute cpu-heavy onion decoding in parallel.
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Add(1)
		go func(seqNum uint16) {
			defer wg.Done()

			onionPkt := &onionPkts[seqNum]

			resps[seqNum].FailCode = decode(
				seqNum, onionPkt, reqs[seqNum],
			)
		}(uint16(i))
	}
	wg.Wait()

	// With that batch created, we will now attempt to write the shared
	// secrets to disk. This operation will returns the set of indices that
	// were detected as replays, and the computed sphinx packets for all
	// indices that did not fail the above loop. Only indices that are not
	// in the replay set should be considered valid, as they are
	// opportunistically computed.
	packets, replays, err := tx.Commit()
	if err != nil {
		log.Errorf("unable to process onion packet batch %x: %v",
			id, err)

		// If we failed to commit the batch to the secret share log, we
		// will mark all not-yet-failed channels with a temporary
		// channel failure and exit since we cannot proceed.
		for i := range resps {
			resp := &resps[i]

			// Skip any indexes that already failed onion decoding.
			if resp.FailCode != lnwire.CodeNone {
				continue
			}

			log.Errorf("unable to process onion packet %x-%v",
				id, i)
			resp.FailCode = lnwire.CodeTemporaryChannelFailure
		}

		// TODO(conner): return real errors to caller so link can fail?
		return resps, err
	}

	// Otherwise, the commit was successful. Now we will post process any
	// remaining packets, additionally failing any that were included in the
	// replay set.
	for i := range resps {
		resp := &resps[i]

		// Skip any indexes that already failed onion decoding.
		if resp.FailCode != lnwire.CodeNone {
			continue
		}

		// If this index is contained in the replay set, mark it with a
		// temporary channel failure error code. We infer that the
		// offending error was due to a replayed packet because this
		// index was found in the replay set.
		if replays.Contains(uint16(i)) {
			log.Errorf("unable to process onion packet: %v",
				sphinx.ErrReplayedPacket)
			resp.FailCode = lnwire.CodeTemporaryChannelFailure
			continue
		}

		// Finally, construct a hop iterator from our processed sphinx
		// packet, simultaneously caching the original onion packet.
		resp.HopIterator = makeSphinxHopIterator(
			&onionPkts[i], &packets[i], MakeBlindingKit(
				p.router, reqs[i].BlindingPoint,
				// We are the last hop if the next hop if the
				// processed packet's action is to exit.
				packets[i].Action == sphinx.ExitNode,
				reqs[i].IncomingAmount, reqs[i].IncomingCltv,
			),
		)
	}

	return resps, nil
}

// ExtractErrorEncrypter takes an io.Reader which should contain the onion
// packet as original received by a forwarding node and creates an
// ErrorEncrypter instance using the derived shared secret. In the case that en
// error occurs, a lnwire failure code detailing the parsing failure will be
// returned.
func (p *OnionProcessor) ExtractErrorEncrypter(ephemeralKey *btcec.PublicKey) (
	ErrorEncrypter, lnwire.FailCode) {

	onionObfuscator, err := sphinx.NewOnionErrorEncrypter(
		p.router, ephemeralKey, nil,
	)
	if err != nil {
		switch err {
		case sphinx.ErrInvalidOnionVersion:
			return nil, lnwire.CodeInvalidOnionVersion
		case sphinx.ErrInvalidOnionHMAC:
			return nil, lnwire.CodeInvalidOnionHmac
		case sphinx.ErrInvalidOnionKey:
			return nil, lnwire.CodeInvalidOnionKey
		default:
			log.Errorf("unable to process onion packet: %v", err)
			return nil, lnwire.CodeInvalidOnionKey
		}
	}

	return &SphinxErrorEncrypter{
		OnionErrorEncrypter: onionObfuscator,
		EphemeralKey:        ephemeralKey,
	}, lnwire.CodeNone
}
