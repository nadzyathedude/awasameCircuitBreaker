package circuitbreaker

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Config holds the configuration for a CircuitBreaker.
type Config struct {
	// Name identifies this breaker in logs and metrics.
	Name string

	// WindowSize is the number of outcomes kept in the sliding window.
	// Default: 20.
	WindowSize int

	// FailureThreshold is the failure ratio (0.0–1.0) that triggers
	// the transition from Closed to Open. Default: 0.5.
	FailureThreshold float64

	// MinRequests is the minimum number of recorded outcomes required
	// before the breaker can trip. Default: 5.
	MinRequests int

	// RecoveryTimeout is how long the breaker stays Open before
	// transitioning to Half-Open. Default: 30s.
	RecoveryTimeout time.Duration

	// ProbeCount is the number of successful probe requests required
	// in Half-Open to transition back to Closed. Default: 3.
	ProbeCount int

	// Fallback is called instead of returning ErrCircuitOpen when the
	// breaker is Open. It receives the context and the circuit-open error.
	Fallback func(ctx context.Context, err error) (any, error)

	// OnStateChange is called whenever the breaker changes state.
	OnStateChange func(name string, from, to State)
}

func (c *Config) withDefaults() Config {
	cfg := *c
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 20
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 0.5
	}
	if cfg.MinRequests <= 0 {
		cfg.MinRequests = 5
	}
	if cfg.RecoveryTimeout <= 0 {
		cfg.RecoveryTimeout = 30 * time.Second
	}
	if cfg.ProbeCount <= 0 {
		cfg.ProbeCount = 3
	}
	return cfg
}

// CircuitBreaker protects function calls using the circuit breaker pattern.
type CircuitBreaker struct {
	cfg Config

	mu              sync.Mutex
	state           State
	window          *slidingWindow
	openedAt        time.Time
	lastStateChange time.Time
	probeSuccesses  int

	totalRequests  atomic.Int64
	totalSuccesses atomic.Int64
	totalFailures  atomic.Int64

	// now is a clock function, overridable for testing.
	now func() time.Time
}

// New creates a CircuitBreaker with the given configuration.
// Zero-value fields in cfg are replaced with sensible defaults.
func New(cfg Config) *CircuitBreaker {
	cfg = cfg.withDefaults()
	return &CircuitBreaker{
		cfg:             cfg,
		state:           StateClosed,
		window:          newSlidingWindow(cfg.WindowSize),
		lastStateChange: time.Now(),
		now:             time.Now,
	}
}

// Execute runs fn through the circuit breaker. If the breaker is Open,
// it returns ErrCircuitOpen (or calls the fallback if configured).
// Context cancellation errors are not recorded as failures.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error) {
	cb.totalRequests.Add(1)

	if err := cb.beforeCall(); err != nil {
		cb.totalFailures.Add(1)
		if cb.cfg.Fallback != nil {
			return cb.cfg.Fallback(ctx, err)
		}
		return nil, err
	}

	result, err := fn(ctx)

	if err != nil && ctx.Err() != nil {
		// Context was cancelled — don't count this outcome.
		return result, err
	}

	cb.afterCall(err)
	if err != nil {
		cb.totalFailures.Add(1)
	} else {
		cb.totalSuccesses.Add(1)
	}

	return result, err
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check if Open has timed out and should become Half-Open.
	if cb.state == StateOpen && cb.now().Sub(cb.openedAt) >= cb.cfg.RecoveryTimeout {
		cb.setState(StateHalfOpen)
	}
	return cb.state
}

// Metrics returns a snapshot of the breaker's runtime statistics.
func (cb *CircuitBreaker) Metrics() Metrics {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return Metrics{
		TotalRequests:     cb.totalRequests.Load(),
		TotalSuccesses:    cb.totalSuccesses.Load(),
		TotalFailures:     cb.totalFailures.Load(),
		CurrentState:      cb.state,
		LastStateChange:   cb.lastStateChange,
		WindowFailureRate: cb.window.failureRate(),
	}
}

// beforeCall checks whether the call is allowed.
// Returns ErrCircuitOpen if the breaker is Open.
func (cb *CircuitBreaker) beforeCall() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		if cb.now().Sub(cb.openedAt) >= cb.cfg.RecoveryTimeout {
			cb.setState(StateHalfOpen)
			return nil // allow probe
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		return nil // probes allowed
	}

	return nil
}

// afterCall records the outcome and performs state transitions.
func (cb *CircuitBreaker) afterCall(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		if err != nil {
			cb.window.record(failure)
		} else {
			cb.window.record(success)
		}

		if cb.window.total() >= cb.cfg.MinRequests &&
			cb.window.failureRate() >= cb.cfg.FailureThreshold {
			cb.setState(StateOpen)
			cb.openedAt = cb.now()
		}

	case StateHalfOpen:
		if err != nil {
			cb.setState(StateOpen)
			cb.openedAt = cb.now()
			cb.probeSuccesses = 0
		} else {
			cb.probeSuccesses++
			if cb.probeSuccesses >= cb.cfg.ProbeCount {
				cb.setState(StateClosed)
				cb.window.reset()
				cb.probeSuccesses = 0
			}
		}
	}
}

// setState transitions the breaker and fires callbacks/logging.
func (cb *CircuitBreaker) setState(to State) {
	from := cb.state
	if from == to {
		return
	}

	cb.state = to
	cb.lastStateChange = cb.now()

	slog.Warn("circuit breaker state change",
		"name", cb.cfg.Name,
		"from", from.String(),
		"to", to.String(),
	)

	if cb.cfg.OnStateChange != nil {
		cb.cfg.OnStateChange(cb.cfg.Name, from, to)
	}
}

// Execute is a generic wrapper around CircuitBreaker.Execute that provides
// type-safe return values.
func Execute[T any](cb *CircuitBreaker, ctx context.Context, fn func(ctx context.Context) (T, error)) (T, error) {
	result, err := cb.Execute(ctx, func(ctx context.Context) (any, error) {
		return fn(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result.(T), nil
}
