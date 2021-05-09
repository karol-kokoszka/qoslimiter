package qoslistener

import (
	"math"

	"golang.org/x/time/rate"
)

func findLimit(bandwidth int) rate.Limit {
	if bandwidth <= AllowAllTraffic {
		return rate.Limit(math.MaxFloat64)
	}
	return rate.Limit(bandwidth)
}

func findBurst(bandwidth int) int {
	if bandwidth <= AllowAllTraffic {
		return 0
	}
	return int(bandwidth)
}
