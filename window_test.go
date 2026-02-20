package circuitbreaker

import (
	"testing"
)

func TestSlidingWindow(t *testing.T) {
	t.Parallel()

	t.Run("empty window has zero failure rate", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(10)

		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0", got)
		}
		if got := w.total(); got != 0 {
			t.Errorf("total() = %v, want 0", got)
		}
	})

	t.Run("records successes correctly", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(5)

		for i := 0; i < 5; i++ {
			w.record(success)
		}

		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0", got)
		}
		if got := w.total(); got != 5 {
			t.Errorf("total() = %v, want 5", got)
		}
	})

	t.Run("records failures correctly", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(5)

		for i := 0; i < 5; i++ {
			w.record(failure)
		}

		if got := w.failureRate(); got != 1.0 {
			t.Errorf("failureRate() = %v, want 1.0", got)
		}
	})

	t.Run("mixed outcomes calculate correct rate", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(10)

		// 3 failures, 7 successes → 30% failure rate.
		for i := 0; i < 3; i++ {
			w.record(failure)
		}
		for i := 0; i < 7; i++ {
			w.record(success)
		}

		if got := w.failureRate(); got != 0.3 {
			t.Errorf("failureRate() = %v, want 0.3", got)
		}
		if got := w.total(); got != 10 {
			t.Errorf("total() = %v, want 10", got)
		}
	})

	t.Run("ring buffer overwrites oldest entries", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(5)

		// Fill with failures.
		for i := 0; i < 5; i++ {
			w.record(failure)
		}
		if got := w.failureRate(); got != 1.0 {
			t.Fatalf("failureRate() = %v, want 1.0", got)
		}

		// Overwrite all failures with successes.
		for i := 0; i < 5; i++ {
			w.record(success)
		}

		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0", got)
		}
		if got := w.total(); got != 5 {
			t.Errorf("total() = %v, want 5 (stays capped at window size)", got)
		}
	})

	t.Run("partial overwrite tracks counts correctly", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(4)

		// [F, F, S, S] → 50% failure.
		w.record(failure)
		w.record(failure)
		w.record(success)
		w.record(success)

		if got := w.failureRate(); got != 0.5 {
			t.Fatalf("failureRate() = %v, want 0.5", got)
		}

		// Overwrite first failure with success → [S, F, S, S] → 25%.
		w.record(success)
		if got := w.failureRate(); got != 0.25 {
			t.Errorf("failureRate() = %v, want 0.25", got)
		}

		// Overwrite second failure with success → [S, S, S, S] → 0%.
		w.record(success)
		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0", got)
		}
	})

	t.Run("window size 1", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(1)

		w.record(failure)
		if got := w.failureRate(); got != 1.0 {
			t.Errorf("failureRate() = %v, want 1.0", got)
		}

		w.record(success)
		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0", got)
		}
	})

	t.Run("reset clears window", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(5)

		for i := 0; i < 5; i++ {
			w.record(failure)
		}
		w.reset()

		if got := w.total(); got != 0 {
			t.Errorf("total() = %v, want 0 after reset", got)
		}
		if got := w.failureRate(); got != 0 {
			t.Errorf("failureRate() = %v, want 0 after reset", got)
		}
	})

	t.Run("zero or negative size defaults to 1", func(t *testing.T) {
		t.Parallel()
		w := newSlidingWindow(0)

		w.record(failure)
		if got := w.total(); got != 1 {
			t.Errorf("total() = %v, want 1", got)
		}
	})
}
