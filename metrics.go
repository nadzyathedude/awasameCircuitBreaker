package circuitbreaker

import "time"

// Metrics holds runtime statistics for a circuit breaker.
type Metrics struct {
	TotalRequests     int64
	TotalSuccesses    int64
	TotalFailures     int64
	CurrentState      State
	LastStateChange   time.Time
	WindowFailureRate float64
}
