package congestion

import (
	"math"
	"time"

	"github.com/lightningnetwork/lnd/fn/v2"
)

// RevenueAverage tracks the average revenue of a channel over multiple windows
// of time to smooth out this value over time. The number of windows that this
// average is tracked over is determined by windowCount.
//
// For example: if we're interested in tracking revenue over two weeks and we're
// interested in aggregating over ten windows, we will track the aggregate
// revenue over the last ten two week windows.
type RevenueAverage struct {
	// startTime tracks when the average started to be tracked. Used to
	// track the actual number of windows we've been tracking for when we
	// haven't yet reached the full windowCount. This gives us some
	// robustness on startup, rather than underestimating.
	//
	// For example: if we've only been tracking for two windows of time,
	// and we're averaging over ten windows we only want to average across
	// the two tracked windows (rather than averaging over ten and including
	// eight windows that are effectively zero).
	startTime time.Time

	// windowCount is the number of windows that we want to track our
	// average revenue.
	windowCount uint8

	// windowDuration is the length of the window we're tracking average
	// values for.
	windowDuration time.Duration

	// aggregatedRevenueDecaying tracks the channel's average incoming
	// revenue over the full period of time that we're interested in
	// aggregating. This is a decent approximation of tracking each window
	// separately, and saves us needing to store multiple data points per
	// channel.
	//
	// For example:
	// - 2 week revenue period
	// - 12 windowCount
	//
	// aggregatedRevenueDecaying will track average revenue over 24 weeks.
	// The two week revenue window revenue average can then be obtained by
	// adjusting for the window size, which has the effect of evenly
	// distributing revenue between the windows.
	aggregatedRevenueDecaying *DecayingAverage
}

// NewRevenueAverage creates a new RevenueAverage instance.
func NewRevenueAverage(revenueWindow time.Duration,
	reputationMultiplier uint8, startTime time.Time,
	startValue fn.Option[int64]) (*RevenueAverage, error) {

	r := &RevenueAverage{
		startTime:      startTime,
		windowCount:    reputationMultiplier,
		windowDuration: revenueWindow,
		aggregatedRevenueDecaying: NewDecayingAverage(
			revenueWindow * time.Duration(reputationMultiplier),
		),
	}

	// If a start value is provided, add it to the decaying average.
	var err error
	startValue.WhenSome(func(value int64) {
		_, err = r.AddValue(value, startTime)
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

// AddValue adds a value to the revenue average at the specified time. The
// updateTime must be after the last updated time of the decaying average,
// tolerant to nanosecond differences.
func (r *RevenueAverage) AddValue(value int64, updateTime time.Time) (int64, error) {
	return r.aggregatedRevenueDecaying.AddValue(value, updateTime)
}

// windowsTracked returns the number of full windows that have been tracked
// since the average started. Returned as a float so that the average can be
// gradually scaled.
func (r *RevenueAverage) windowsTracked(accessTime time.Time) float64 {
	return accessTime.Sub(r.startTime).Seconds() /
		r.windowDuration.Seconds()
}

// ValueAtInstant updates the current value of the decaying average and returns
// the value adjusted for the number of windows tracked. The value provided will
// act as a saturating add if it exceeds int64 max.
func (r *RevenueAverage) ValueAtInstant(accessTime time.Time) (int64, error) {
	// If we're below our count of windows, we only want to aggregate for
	// the amount of windows we've tracked so far. If we've reached our
	// count, we just use that because the average only tracks this number
	// of windows.
	windowsTracked := r.windowsTracked(accessTime)
	windowDivisor := math.Min(
		// If less than one window has been tracked, this will be a
		// fraction which will inflate our revenue so we just flatten
		// it to 1.
		// TODO: better strategy for first window?
		func() float64 {
			if windowsTracked < 1.0 {
				return 1.0
			}
			return windowsTracked
		}(),
		float64(r.windowCount),
	)

	// To give the value for this longer-running average over an equivalent
	// two week period, we just divide it by the number of windows we're
	// counting.
	aggregatedValue, err := r.aggregatedRevenueDecaying.ValueAtInstant(
		accessTime,
	)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(float64(aggregatedValue) / windowDivisor)), nil
}
