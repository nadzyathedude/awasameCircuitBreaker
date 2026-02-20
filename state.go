package circuitbreaker

// State represents the current state of a circuit breaker.
type State int

const (
	// StateClosed is the normal operating state. Requests pass through
	// and failures are tracked in the sliding window.
	StateClosed State = iota

	// StateOpen rejects all requests immediately with ErrCircuitOpen.
	// After RecoveryTimeout elapses, transitions to StateHalfOpen.
	StateOpen

	// StateHalfOpen allows a limited number of probe requests through.
	// If all probes succeed, transitions to StateClosed.
	// If any probe fails, transitions back to StateOpen.
	StateHalfOpen
)

// String returns the string representation of a State.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}
