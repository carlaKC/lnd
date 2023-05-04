package lnd

import (
	"math"
	"time"
)

var reasonableResolution = time.Second * 10

func reputationDelta(endorsed, success bool, fees int64, resolution time.Duration) int64 {
	opportunityCost := int64(
		math.Ceil(float64(resolution-reasonableResolution)/float64(reasonableResolution)),
	) * fees

	switch {
	case endorsed && success:
		// Fast: + fees
		// Slow: + fees - opportunity cost for additional periods held.
		return fees - opportunityCost

	case endorsed && !success:
		// Fast: - fees
		// Slow: - fees - opportunity cost for additional periods held.
		return (fees + opportunityCost) * -1

	case !endorsed:
		// Fast success: + fees
		// Otherwise: 0
		fastResolution := resolution <= reasonableResolution
		if success && fastResolution {
			return fees
		}

		return 0
	}

	return 0
}
