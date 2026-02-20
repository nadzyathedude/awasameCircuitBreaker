package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// helper to create a breaker with a controllable clock.
func newTestBreaker(cfg Config) (*CircuitBreaker, *fakeClock) {
	cb := New(cfg)
	fc := &fakeClock{t: time.Now()}
	cb.now = fc.Now
	return cb, fc
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (fc *fakeClock) Now() time.Time {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.t
}

func (fc *fakeClock) Advance(d time.Duration) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.t = fc.t.Add(d)
}

var errBoom = errors.New("boom")

func succeedFn(_ context.Context) (any, error) { return "ok", nil }
func failFn(_ context.Context) (any, error)    { return nil, errBoom }

func TestCircuitBreaker(t *testing.T) {
	t.Parallel()

	t.Run("Closed: requests pass through", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{Name: "test"})

		res, err := cb.Execute(context.Background(), succeedFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res != "ok" {
			t.Fatalf("result = %v, want ok", res)
		}
		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed", cb.State())
		}
	})

	t.Run("Closed: successes do not change state", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{Name: "test", WindowSize: 5, MinRequests: 5})

		for i := 0; i < 10; i++ {
			if _, err := cb.Execute(context.Background(), succeedFn); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed", cb.State())
		}
	})

	t.Run("Closed→Open: failures exceed threshold", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       10,
			FailureThreshold: 0.5,
			MinRequests:      5,
		})

		// 5 failures → 100% failure rate → opens.
		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		if cb.State() != StateOpen {
			t.Fatalf("state = %v, want Open", cb.State())
		}
	})

	t.Run("Open: requests are rejected with ErrCircuitOpen", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  1 * time.Minute,
		})

		// Trip the breaker.
		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		_, err := cb.Execute(context.Background(), succeedFn)
		if !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("err = %v, want ErrCircuitOpen", err)
		}
	})

	t.Run("Open→HalfOpen: after RecoveryTimeout", func(t *testing.T) {
		t.Parallel()
		cb, fc := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  10 * time.Second,
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}
		if cb.State() != StateOpen {
			t.Fatalf("state = %v, want Open", cb.State())
		}

		fc.Advance(11 * time.Second)

		if got := cb.State(); got != StateHalfOpen {
			t.Fatalf("state = %v, want HalfOpen", got)
		}
	})

	t.Run("HalfOpen→Closed: all probes succeed", func(t *testing.T) {
		t.Parallel()
		cb, fc := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  10 * time.Second,
			ProbeCount:       3,
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		fc.Advance(11 * time.Second)

		// 3 successful probes → should close.
		for i := 0; i < 3; i++ {
			if _, err := cb.Execute(context.Background(), succeedFn); err != nil {
				t.Fatalf("probe %d: unexpected error: %v", i, err)
			}
		}

		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed", cb.State())
		}
	})

	t.Run("HalfOpen→Open: probe fails", func(t *testing.T) {
		t.Parallel()
		cb, fc := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  10 * time.Second,
			ProbeCount:       3,
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		fc.Advance(11 * time.Second)

		// First probe succeeds, second fails → back to Open.
		cb.Execute(context.Background(), succeedFn)
		cb.Execute(context.Background(), failFn)

		if cb.State() != StateOpen {
			t.Fatalf("state = %v, want Open", cb.State())
		}
	})

	t.Run("MinRequests: breaker does not trip below minimum", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       10,
			FailureThreshold: 0.5,
			MinRequests:      5,
		})

		// 4 failures, still under MinRequests.
		for i := 0; i < 4; i++ {
			cb.Execute(context.Background(), failFn)
		}

		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed (under MinRequests)", cb.State())
		}
	})

	t.Run("Fallback: called when breaker is Open", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  1 * time.Minute,
			Fallback: func(_ context.Context, err error) (any, error) {
				return "fallback", nil
			},
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		res, err := cb.Execute(context.Background(), succeedFn)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res != "fallback" {
			t.Fatalf("result = %v, want fallback", res)
		}
	})

	t.Run("Fallback: not called when Closed and function fails", func(t *testing.T) {
		t.Parallel()
		called := false
		cb, _ := newTestBreaker(Config{
			Name: "test",
			Fallback: func(_ context.Context, err error) (any, error) {
				called = true
				return "fallback", nil
			},
		})

		_, err := cb.Execute(context.Background(), failFn)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if called {
			t.Fatal("fallback should not be called when breaker is Closed")
		}
	})

	t.Run("OnStateChange: called with correct arguments", func(t *testing.T) {
		t.Parallel()

		type transition struct {
			name     string
			from, to State
		}
		var mu sync.Mutex
		var transitions []transition

		cb, _ := newTestBreaker(Config{
			Name:             "my-breaker",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			OnStateChange: func(name string, from, to State) {
				mu.Lock()
				transitions = append(transitions, transition{name, from, to})
				mu.Unlock()
			},
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		mu.Lock()
		defer mu.Unlock()
		if len(transitions) != 1 {
			t.Fatalf("got %d transitions, want 1", len(transitions))
		}
		tr := transitions[0]
		if tr.name != "my-breaker" || tr.from != StateClosed || tr.to != StateOpen {
			t.Fatalf("transition = %+v, want {my-breaker Closed→Open}", tr)
		}
	})

	t.Run("Context cancellation: not recorded as failure", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       10,
			FailureThreshold: 0.5,
			MinRequests:      5,
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancel

		for i := 0; i < 10; i++ {
			cb.Execute(ctx, func(ctx context.Context) (any, error) {
				return nil, ctx.Err()
			})
		}

		// Should still be closed because context errors are not counted.
		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed (context errors not counted)", cb.State())
		}

		m := cb.Metrics()
		if m.WindowFailureRate != 0 {
			t.Fatalf("WindowFailureRate = %v, want 0", m.WindowFailureRate)
		}
	})

	t.Run("Concurrent access: no data races", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       100,
			FailureThreshold: 0.9,
			MinRequests:      50,
		})

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				fn := succeedFn
				if n%3 == 0 {
					fn = failFn
				}
				cb.Execute(context.Background(), fn)
				cb.State()
				cb.Metrics()
			}(i)
		}
		wg.Wait()
	})

	t.Run("Generics: Execute[T] returns correct type", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{Name: "test"})

		result, err := Execute[string](cb, context.Background(), func(_ context.Context) (string, error) {
			return "typed-value", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "typed-value" {
			t.Fatalf("result = %v, want typed-value", result)
		}
	})

	t.Run("Generics: Execute[T] returns zero value on error", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{Name: "test"})

		result, err := Execute[int](cb, context.Background(), func(_ context.Context) (int, error) {
			return 0, errBoom
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if result != 0 {
			t.Fatalf("result = %v, want 0", result)
		}
	})

	t.Run("Metrics: tracks totals correctly", func(t *testing.T) {
		t.Parallel()
		cb, _ := newTestBreaker(Config{
			Name:        "test",
			MinRequests: 100, // high enough to never trip
		})

		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), succeedFn)
		}
		for i := 0; i < 3; i++ {
			cb.Execute(context.Background(), failFn)
		}

		m := cb.Metrics()
		if m.TotalRequests != 8 {
			t.Errorf("TotalRequests = %d, want 8", m.TotalRequests)
		}
		if m.TotalSuccesses != 5 {
			t.Errorf("TotalSuccesses = %d, want 5", m.TotalSuccesses)
		}
		if m.TotalFailures != 3 {
			t.Errorf("TotalFailures = %d, want 3", m.TotalFailures)
		}
		if m.CurrentState != StateClosed {
			t.Errorf("CurrentState = %v, want Closed", m.CurrentState)
		}
	})

	t.Run("Closed→Open resets on HalfOpen→Closed transition", func(t *testing.T) {
		t.Parallel()
		cb, fc := newTestBreaker(Config{
			Name:             "test",
			WindowSize:       5,
			FailureThreshold: 0.5,
			MinRequests:      5,
			RecoveryTimeout:  10 * time.Second,
			ProbeCount:       1,
		})

		// Trip the breaker.
		for i := 0; i < 5; i++ {
			cb.Execute(context.Background(), failFn)
		}

		// Recover.
		fc.Advance(11 * time.Second)
		cb.Execute(context.Background(), succeedFn) // probe succeeds → Closed

		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed", cb.State())
		}

		// Window should be reset — new failures should not immediately trip.
		for i := 0; i < 4; i++ {
			cb.Execute(context.Background(), failFn)
		}
		if cb.State() != StateClosed {
			t.Fatalf("state = %v, want Closed (window was reset)", cb.State())
		}
	})
}

func TestStateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state State
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
