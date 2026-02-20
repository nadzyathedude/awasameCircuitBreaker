package circuitbreaker

import "errors"

// ErrCircuitOpen is returned when the circuit breaker is in the Open state
// and rejects the request without executing the wrapped function.
var ErrCircuitOpen = errors.New("circuit breaker is open")
