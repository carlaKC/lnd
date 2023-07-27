package record

import (
	"fmt"

	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// CustomTypeStart is the start of the custom tlv type range as defined
	// in BOLT 01.
	CustomTypeStart = 65536
)

// CustomSet stores a set of custom key/value pairs.
type CustomSet map[uint64][]byte

// Validate checks that all custom records are in the custom type range.
func (c CustomSet) Validate() error {
	for key := range c {
		if key < CustomTypeStart {
			return fmt.Errorf("no custom records with types "+
				"below %v allowed", CustomTypeStart)
		}
	}

	return nil
}

// NewCustomRecords filters the types parsed from the tlv stream for custom
// records.
func NewCustomRecords(parsedTypes tlv.TypeMap) CustomSet {
	customRecords := make(CustomSet)
	for t, parseResult := range parsedTypes {
		if parseResult == nil || t < CustomTypeStart {
			continue
		}
		customRecords[uint64(t)] = parseResult
	}
	return customRecords
}
