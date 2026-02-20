package circuitbreaker

import (
	"sync"
	"testing"
)

func TestRegistry(t *testing.T) {
	t.Parallel()

	t.Run("Get: creates new breaker if not exists", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{WindowSize: 10})

		cb := r.Get("svc-a")
		if cb == nil {
			t.Fatal("Get returned nil")
		}
	})

	t.Run("Get: returns existing breaker", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{WindowSize: 10})

		cb1 := r.Get("svc-a")
		cb2 := r.Get("svc-a")
		if cb1 != cb2 {
			t.Fatal("Get returned different instances for the same name")
		}
	})

	t.Run("Get: sets Name from key", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{WindowSize: 10})

		cb := r.Get("my-service")
		if cb.cfg.Name != "my-service" {
			t.Fatalf("cfg.Name = %q, want %q", cb.cfg.Name, "my-service")
		}
	})

	t.Run("GetWithConfig: creates with custom config", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{WindowSize: 10})

		cb := r.GetWithConfig("custom", Config{WindowSize: 42})
		if cb.cfg.WindowSize != 42 {
			t.Fatalf("WindowSize = %d, want 42", cb.cfg.WindowSize)
		}
	})

	t.Run("GetWithConfig: returns existing breaker ignoring new config", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{WindowSize: 10})

		cb1 := r.GetWithConfig("svc", Config{WindowSize: 42})
		cb2 := r.GetWithConfig("svc", Config{WindowSize: 99})
		if cb1 != cb2 {
			t.Fatal("GetWithConfig returned different instances")
		}
		if cb2.cfg.WindowSize != 42 {
			t.Fatalf("WindowSize = %d, want 42 (original)", cb2.cfg.WindowSize)
		}
	})

	t.Run("All: returns all registered breakers", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{})

		r.Get("a")
		r.Get("b")
		r.Get("c")

		all := r.All()
		if len(all) != 3 {
			t.Fatalf("len(All()) = %d, want 3", len(all))
		}
		for _, name := range []string{"a", "b", "c"} {
			if _, ok := all[name]; !ok {
				t.Errorf("missing breaker %q", name)
			}
		}
	})

	t.Run("All: returns a copy, not a reference", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{})

		r.Get("x")
		all := r.All()
		delete(all, "x")

		// Original should still have "x".
		if _, ok := r.All()["x"]; !ok {
			t.Fatal("deleting from All() affected the registry")
		}
	})

	t.Run("Concurrent access: parallel Get from many goroutines", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{})

		var wg sync.WaitGroup
		breakers := make([]*CircuitBreaker, 100)

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				breakers[n] = r.Get("shared")
			}(i)
		}
		wg.Wait()

		// All should be the exact same pointer.
		for i := 1; i < 100; i++ {
			if breakers[i] != breakers[0] {
				t.Fatalf("goroutine %d got a different breaker instance", i)
			}
		}
	})

	t.Run("Concurrent access: different keys from many goroutines", func(t *testing.T) {
		t.Parallel()
		r := NewRegistry(Config{})

		var wg sync.WaitGroup
		names := []string{"a", "b", "c", "d", "e"}

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				r.Get(names[n%len(names)])
			}(i)
		}
		wg.Wait()

		all := r.All()
		if len(all) != len(names) {
			t.Fatalf("len(All()) = %d, want %d", len(all), len(names))
		}
	})
}
