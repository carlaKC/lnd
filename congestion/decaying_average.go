package congestion

import (
	"fmt"
	"math"
	"time"

	"github.com/lightningnetwork/lnd/fn/v2"
)

// DecayingAverage tracks a timestamped decaying average, which may be positive
// or negative.
type DecayingAverage struct {
	value       int64
	lastUpdated fn.Option[time.Time]
	decayRate   float64
}

// NewDecayingAverage creates a new decaying average with the given decay
// period.
func NewDecayingAverage(period time.Duration) *DecayingAverage {
	return &DecayingAverage{
		value:       0,
		lastUpdated: fn.None[time.Time](),
		decayRate:   calcDecayRate(period),
	}
}

// calcDecayRate calculates the decay rate from a period.
func calcDecayRate(period time.Duration) float64 {
	return math.Pow(0.5, 2.0/period.Seconds())
}

// ValueAtInstant decays the tracked value to its value at the instant provided
// and returns the updated value. The accessTime must be after the lastUpdated
// time of the decaying average, tolerant to nanosecond differences.
func (d *DecayingAverage) ValueAtInstant(accessTime time.Time) (int64, error) {
	d.lastUpdated.WhenSome(func(lastUpdated time.Time) {
		// Enforce that the accessTime must be after the last update on
		// our average, but tolerate nanosecond differences - these will
		// just reflect as an update with the same update as lastUpdated.
		if accessTime.Before(lastUpdated) &&
			lastUpdated.Sub(accessTime).Seconds() > 0 {

			return
		}

		elapsed := accessTime.Sub(lastUpdated).Seconds()
		d.value = int64(math.Round(
			float64(d.value) * math.Pow(d.decayRate, elapsed),
		))
	})

	// Check if we need to return an error for time going backwards.
	// We need to do this check again outside WhenSome to be able to
	// return the error.
	lastUpdated, err := d.lastUpdated.UnwrapOrErr(nil)
	if err == nil {
		if accessTime.Before(lastUpdated) &&
			lastUpdated.Sub(accessTime).Seconds() > 0 {

			return 0, fmt.Errorf("update in past: last_updated=%v, "+
				"access_time=%v", lastUpdated, accessTime)
		}
	}

	d.lastUpdated = fn.Some(accessTime)
	return d.value, nil
}

// AddValue updates the current value of the decaying average and then adds the
// new value provided. The value provided will act as a saturating add if it
// exceeds int64 max.
func (d *DecayingAverage) AddValue(value int64, updateTime time.Time) (int64, error) {
	// Progress current value to the new timestamp so that it'll be
	// appropriately decayed.
	_, err := d.ValueAtInstant(updateTime)
	if err != nil {
		return 0, err
	}

	// No need to decay the new value as we're now at our last updated time.
	d.value = saturatingAddInt64(d.value, value)
	d.lastUpdated = fn.Some(updateTime)

	return d.value, nil
}

// saturatingAddInt64 performs saturating addition on int64 values.
func saturatingAddInt64(a, b int64) int64 {
	c := a + b

	// Check for overflow
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}

	// Check for underflow
	if b < 0 && a < math.MinInt64-b {
		return math.MinInt64
	}

	return c
}
