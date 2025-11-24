package congestion

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/lightningnetwork/lnd/fn/v2"
	"github.com/lightningnetwork/lnd/lnwire"
)

// ErrChannelNotFound is returned when a channel is not found in the bucket.
var ErrChannelNotFound = fmt.Errorf("channel not found")

// ErrBucketTooEmpty is returned when trying to remove more from a bucket than
// is occupied.
var ErrBucketTooEmpty = fmt.Errorf("bucket too empty")

// AssignedSlots defines the number of slots each candidate channel is allowed
// in the general bucket. This value assumes that we're operating with a
// protocol limit of 483 htlcs (not 120, as in V3).
const AssignedSlots = 20

// BucketParameters describes the size of a resource bucket.
type BucketParameters struct {
	// SlotCount is the number of HTLC slots available in the bucket.
	SlotCount uint16

	// LiquidityMsat is the amount of liquidity available in the bucket.
	LiquidityMsat uint64
}

// slotUsage tracks a slot index and whether it's currently in use.
type slotUsage struct {
	index uint16
	inUse bool
}

// IncomingChannel tracks resources available on the channel when it is
// utilized as the incoming direction in a htlc forward.
type IncomingChannel struct {
	// generalBucket holds the resources available for htlcs that are not
	// accountable, or are not sent by a peer with sufficient reputation.
	generalBucket *GeneralBucket

	// congestionBucket holds the resources available for htlcs that are
	// accountable from peers that do not have sufficient reputation. This
	// bucket is only used when the general bucket is full, and peers are
	// limited to a single slot/liquidity block.
	congestionBucket BucketParameters

	// protectedBucket holds the resources available on the protected
	// bucket. This will be used by htlcs that are accountable from peers
	// that have sufficient reputation.
	protectedBucket BucketParameters

	// protectedSlotsUsed tracks the number of slots currently in use in
	// the protected bucket.
	protectedSlotsUsed uint16

	// protectedLiquidityUsed tracks the amount of liquidity currently in
	// use in the protected bucket.
	protectedLiquidityUsed uint64

	// congestionSlotsUsed tracks the number of slots currently in use in
	// the congestion bucket.
	congestionSlotsUsed uint16

	// congestionLiquidityUsed tracks the amount of liquidity currently in
	// use in the congestion bucket.
	congestionLiquidityUsed uint64

	// revenue tracks the revenue that this node has earned us as the
	// incoming forwarder.
	revenue *RevenueAverage
}

// NewIncomingChannel creates a new IncomingChannel with the specified bucket
// parameters.
func NewIncomingChannel(params *ReputationParams, scid lnwire.ShortChannelID,
	generalBucket, congestionBucket, protectedBucket BucketParameters,
	startTime time.Time, startState fn.Option[int64]) (*IncomingChannel, error) {

	gb, err := NewGeneralBucket(scid, generalBucket)
	if err != nil {
		return nil, err
	}

	revenue, err := NewRevenueAverage(
		params.RevenueWindow, params.ReputationMultiplier,
		startTime, startState,
	)
	if err != nil {
		return nil, err
	}

	return &IncomingChannel{
		generalBucket:    gb,
		congestionBucket: congestionBucket,
		protectedBucket:  protectedBucket,
		revenue:          revenue,
	}, nil
}

// BucketType represents which bucket a HTLC has been assigned to.
type BucketType uint8

const (
	// GeneralBucketType indicates the HTLC is in the general bucket.
	GeneralBucketType BucketType = iota

	// ProtectedBucketType indicates the HTLC is in the protected bucket.
	ProtectedBucketType

	// CongestionBucketType indicates the HTLC is in the congestion bucket.
	CongestionBucketType
)

// AddHTLC attempts to add a HTLC to the appropriate resource bucket based on
// the accountability signal and outgoing peer reputation. It returns:
//   - fn.Some(true) if the HTLC should be forwarded with accountable=true
//     (protected or congestion bucket)
//   - fn.Some(false) if the HTLC should be forwarded with accountable=false
//     (general bucket)
//   - fn.None[bool]() if the HTLC cannot be accommodated
//
// The bucket assignment follows this logic:
// - If incomingAccountable is set:
//   - If outgoing peer has reputation: assign to protected bucket → Some(true)
//   - Otherwise: fail (return None)
//
// - Otherwise (unaccountable):
//   - If general bucket has space: assign to general bucket → Some(false)
//   - Otherwise:
//   - If outgoing peer has reputation: assign to protected bucket → Some(true)
//   - Otherwise: try congestion bucket → Some(true), fail if no space
func (ic *IncomingChannel) AddHTLC(
	outgoingChannel lnwire.ShortChannelID,
	amountMsat uint64,
	incomingAccountable AccountableSignal,
	outgoingHasReputation bool,
) (fn.Option[bool], BucketType, error) {

	if incomingAccountable == Accountable {
		if outgoingHasReputation {
			if ic.allocateProtected(amountMsat) {
				return fn.Some(true), ProtectedBucketType, nil
			}
			// TODO(CKC): if protected bucket is full fall back?
			return fn.None[bool](), 0, nil
		}

		return fn.None[bool](), 0, nil
	}

	added, err := ic.generalBucket.AddHTLC(outgoingChannel, amountMsat)
	if err != nil {
		return fn.None[bool](), 0, err
	}
	if added {
		return fn.Some(false), GeneralBucketType, nil
	}

	if outgoingHasReputation {
		if ic.allocateProtected(amountMsat) {
			return fn.Some(true), ProtectedBucketType, nil
		}
	}

	if ic.allocateCongestion(amountMsat) {
		return fn.Some(true), CongestionBucketType, nil
	}

	return fn.None[bool](), 0, nil
}

// allocateProtected attempts to allocate resources from the protected bucket.
// Returns true if successful, false if insufficient resources.
func (ic *IncomingChannel) allocateProtected(amountMsat uint64) bool {
	if ic.protectedSlotsUsed >= ic.protectedBucket.SlotCount {
		return false
	}

	if ic.protectedLiquidityUsed+amountMsat > ic.protectedBucket.LiquidityMsat {
		return false
	}

	ic.protectedSlotsUsed++
	ic.protectedLiquidityUsed += amountMsat

	return true
}

// allocateCongestion attempts to allocate resources from the congestion bucket.
// Returns true if successful, false if insufficient resources.
func (ic *IncomingChannel) allocateCongestion(amountMsat uint64) bool {
	if ic.congestionSlotsUsed >= ic.congestionBucket.SlotCount {
		return false
	}

	if ic.congestionLiquidityUsed+amountMsat > ic.congestionBucket.LiquidityMsat {
		return false
	}

	ic.congestionSlotsUsed++
	ic.congestionLiquidityUsed += amountMsat

	return true
}

// RemoveHTLC releases resources from the specified bucket for a resolved HTLC.
// The bucketType parameter must match the bucket type returned when the HTLC
// was added.
func (ic *IncomingChannel) RemoveHTLC(
	outgoingChannel lnwire.ShortChannelID,
	amountMsat uint64,
	bucketType BucketType,
) error {

	switch bucketType {
	case GeneralBucketType:
		return ic.generalBucket.RemoveHTLC(outgoingChannel, amountMsat)

	case ProtectedBucketType:
		if ic.protectedSlotsUsed == 0 {
			return fmt.Errorf("protected bucket: %w", ErrBucketTooEmpty)
		}
		if ic.protectedLiquidityUsed < amountMsat {
			return fmt.Errorf("protected bucket: %w: amount %d",
				ErrBucketTooEmpty, amountMsat)
		}

		ic.protectedSlotsUsed--
		ic.protectedLiquidityUsed -= amountMsat
		return nil

	case CongestionBucketType:
		if ic.congestionSlotsUsed == 0 {
			return fmt.Errorf("congestion bucket: %w", ErrBucketTooEmpty)
		}
		if ic.congestionLiquidityUsed < amountMsat {
			return fmt.Errorf("congestion bucket: %w: amount %d",
				ErrBucketTooEmpty, amountMsat)
		}

		ic.congestionSlotsUsed--
		ic.congestionLiquidityUsed -= amountMsat
		return nil

	default:
		return fmt.Errorf("unknown bucket type: %d", bucketType)
	}
}

// GeneralBucket manages HTLC slot allocation for the general bucket using
// a hash-based slot assignment strategy.
type GeneralBucket struct {
	// params holds the resources available for htlcs that are not
	// accountable, or are not sent by a peer with sufficient reputation.
	params BucketParameters

	// scid is the short channel ID that represents the channel that the
	// bucket belongs to.
	scid lnwire.ShortChannelID

	// htlcSlots tracks the occupancy of HTLC slots in the bucket.
	htlcSlots []bool

	// slotSizeMsat tracks the amount of liquidity allocated to each slot
	// in the bucket.
	slotSizeMsat uint64

	// candidateSlots maps short channel IDs to an array of the slots that
	// the channel is allowed to use, and their current usage state. This
	// information is required to track exactly which slots to remove
	// liquidity from.
	candidateSlots map[lnwire.ShortChannelID][AssignedSlots]slotUsage
}

// NewGeneralBucket creates a new general bucket.
//
// Note that the current implementation is not restart safe:
//   - It assigns new salt every time a channel is added (should be persisted
//     across restarts).
//   - It assumes that the bucket is empty on start (should account for in-flight
//     HTLCs).
func NewGeneralBucket(scid lnwire.ShortChannelID,
	params BucketParameters) (*GeneralBucket, error) {

	slotSizeMsat := params.LiquidityMsat / uint64(params.SlotCount)
	if slotSizeMsat == 0 {
		return nil, fmt.Errorf("channel size: %d with %d slots "+
			"results in zero liquidity bucket", params.LiquidityMsat,
			params.SlotCount)
	}

	return &GeneralBucket{
		params:         params,
		scid:           scid,
		htlcSlots:      make([]bool, params.SlotCount),
		slotSizeMsat:   slotSizeMsat,
		candidateSlots: make(map[lnwire.ShortChannelID][AssignedSlots]slotUsage),
	}, nil
}

// RemoveChannel removes a channel from internal state, returning a boolean
// indicating whether anything was removed from state.
func (g *GeneralBucket) RemoveChannel(
	candidateScid lnwire.ShortChannelID) bool {

	_, exists := g.candidateSlots[candidateScid]
	delete(g.candidateSlots, candidateScid)
	return exists
}

// getCandidateSlots produces the set of slots that a channel has permission
// to use. Assumes that htlcSlots has been initialized with values set for
// each slot. Retries up to AssignedSlots * 2 times to avoid duplicates, then
// fails (as it's highly improbable that we can't get non-duplicates after
// that many attempts).
func (g *GeneralBucket) getCandidateSlots(
	candidateScid lnwire.ShortChannelID) ([AssignedSlots]uint16, error) {

	if candidateScid == g.scid {
		return [AssignedSlots]uint16{}, fmt.Errorf(
			"can't self-assign slots: %v", candidateScid)
	}

	// Check if we already have slots assigned for this channel.
	if existing, ok := g.candidateSlots[candidateScid]; ok {
		var result [AssignedSlots]uint16
		for i, slot := range existing {
			result[i] = slot.index
		}
		return result, nil
	}

	// Generate a random salt for slot assignment.
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return [AssignedSlots]uint16{}, fmt.Errorf("failed to "+
			"generate salt: %w", err)
	}

	var result [AssignedSlots]slotUsage
	assignedCount := 0

	// We hash the channel pair along with salt and an index to get our
	// slots.
	maxAttempts := AssignedSlots * 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if assignedCount == AssignedSlots {
			break
		}

		// Build the data to hash: salt + scid + candidateScid + attempt
		hash := sha256.New()
		hash.Write(salt[:])
		_ = binary.Write(hash, binary.BigEndian, g.scid.ToUint64())
		_ = binary.Write(hash, binary.BigEndian, candidateScid.ToUint64())
		_ = binary.Write(hash, binary.BigEndian, uint8(attempt))

		hashResult := hash.Sum(nil)

		// Use the first 8 bytes for indexing.
		hashNum := binary.BigEndian.Uint64(hashResult[0:8])

		htlcSlot := uint16(hashNum % uint64(len(g.htlcSlots)))
		candidateSlot := slotUsage{
			index: htlcSlot,
			inUse: false,
		}

		// Check for duplicates.
		isDuplicate := false
		for i := 0; i < assignedCount; i++ {
			if result[i].index == candidateSlot.index {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			result[assignedCount] = candidateSlot
			assignedCount++
		}
	}

	if assignedCount < AssignedSlots {
		return [AssignedSlots]uint16{}, fmt.Errorf("could not assign "+
			"%d unique slots for channel %v, only found %d",
			AssignedSlots, candidateScid, assignedCount)
	}

	// Store the assigned slots.
	g.candidateSlots[candidateScid] = result

	// Return just the indices.
	var indices [AssignedSlots]uint16
	for i, slot := range result {
		indices[i] = slot.index
	}
	return indices, nil
}

// requiredSlotCount returns the number of liquidity slots a HTLC requires.
func (g *GeneralBucket) requiredSlotCount(amountMsat uint64) uint64 {
	required := (amountMsat + g.slotSizeMsat - 1) / g.slotSizeMsat
	if required < 1 {
		return 1
	}
	return required
}

// getUsableSlots returns the indexes of a set of slots that can hold the
// payment amount provided.
func (g *GeneralBucket) getUsableSlots(candidateScid lnwire.ShortChannelID,
	amountMsat uint64) ([]uint16, error) {

	requiredSlotCount := g.requiredSlotCount(amountMsat)
	slots, err := g.getCandidateSlots(candidateScid)
	if err != nil {
		return nil, err
	}

	// Filter for available slots.
	var availableSlots []uint16
	for _, index := range slots {
		if !g.htlcSlots[index] {
			availableSlots = append(availableSlots, index)
		}
	}

	if uint64(len(availableSlots)) < requiredSlotCount {
		return nil, nil
	}

	return availableSlots[:requiredSlotCount], nil
}

// MayAddHTLC checks whether there is space in the bucket to accommodate the
// HTLC amount.
//
// Requires a mutable reference because it may need to opportunistically
// allocate slots to the channel if it has never been used as the outgoing
// forwarding channel with this one. This is done "just in time" so that we
// don't need to pick slots for channels that we may never forward with.
func (g *GeneralBucket) MayAddHTLC(candidateScid lnwire.ShortChannelID,
	amountMsat uint64) (bool, error) {

	slots, err := g.getUsableSlots(candidateScid, amountMsat)
	if err != nil {
		return false, err
	}

	return slots != nil, nil
}

// AddHTLC adds a HTLC to the bucket, returning a boolean indicating whether
// the HTLC was successfully added.
//
// Requires a mutable reference because it may need to opportunistically
// allocate slots to the channel if it has never been used as the outgoing
// forwarding channel with this one. This is done "just in time" so that we
// don't need to pick slots for channels that we may never forward with.
func (g *GeneralBucket) AddHTLC(candidateScid lnwire.ShortChannelID,
	amountMsat uint64) (bool, error) {

	availableSlots, err := g.getUsableSlots(candidateScid, amountMsat)
	if err != nil {
		return false, err
	}
	if availableSlots == nil {
		return false, nil
	}

	// Get the channel's slot assignments.
	channelSlots, ok := g.candidateSlots[candidateScid]
	if !ok {
		return false, fmt.Errorf("%w: %v", ErrChannelNotFound,
			candidateScid)
	}

	// Reserve the specific channel slots we need.
	for _, index := range availableSlots {
		if g.htlcSlots[index] {
			return false, fmt.Errorf("assigning slot already taken")
		}
		g.htlcSlots[index] = true

		// Find and mark the slot in the channel's assignments.
		found := false
		for i := range channelSlots {
			if channelSlots[i].index == index {
				if channelSlots[i].inUse {
					return false, fmt.Errorf("assigning " +
						"slot already taken")
				}
				channelSlots[i].inUse = true
				found = true
				break
			}
		}
		if !found {
			return false, fmt.Errorf("inconsistent slots assigned " +
				"in general bucket")
		}
	}

	// Update the stored channel slots.
	g.candidateSlots[candidateScid] = channelSlots

	return true, nil
}

// RemoveHTLC removes a HTLC for the candidate channel. Should be called once
// the HTLC has been resolved.
func (g *GeneralBucket) RemoveHTLC(candidateScid lnwire.ShortChannelID,
	amountMsat uint64) error {

	requiredSlotCount := g.requiredSlotCount(amountMsat)

	channelSlots, ok := g.candidateSlots[candidateScid]
	if !ok {
		return fmt.Errorf("%w: %v", ErrChannelNotFound, candidateScid)
	}

	// Collect occupied slots.
	var occupiedSlots []slotUsage
	for _, slot := range channelSlots {
		if slot.inUse {
			occupiedSlots = append(occupiedSlots, slot)
		}
	}

	if uint64(len(occupiedSlots)) < requiredSlotCount {
		return fmt.Errorf("%w: amount %d", ErrBucketTooEmpty, amountMsat)
	}

	// Remove the required number of slots.
	for i := uint64(0); i < requiredSlotCount; i++ {
		slot := occupiedSlots[i]

		if !g.htlcSlots[slot.index] {
			return fmt.Errorf("removing unassigned slot")
		}
		g.htlcSlots[slot.index] = false

		// Find and clear the slot in the channel's assignments.
		found := false
		for j := range channelSlots {
			if channelSlots[j].index == slot.index {
				if !channelSlots[j].inUse {
					return fmt.Errorf("removing " +
						"unassigned slot")
				}
				channelSlots[j].inUse = false
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("inconsistent slots assigned in " +
				"general bucket")
		}
	}

	// Update the stored channel slots.
	g.candidateSlots[candidateScid] = channelSlots

	return nil
}
