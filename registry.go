package circuitbreaker

import "sync"

// Registry manages a collection of named circuit breakers.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	breakers  map[string]*CircuitBreaker
	defaultCfg Config
}

// NewRegistry creates a Registry that uses defaultCfg for breakers
// created via Get.
func NewRegistry(defaultCfg Config) *Registry {
	return &Registry{
		breakers:   make(map[string]*CircuitBreaker),
		defaultCfg: defaultCfg,
	}
}

// Get returns the circuit breaker registered under name, creating one
// with the default config if it does not exist. The Name field of the
// config is set to name automatically.
func (r *Registry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if cb, ok = r.breakers[name]; ok {
		return cb
	}

	cfg := r.defaultCfg
	cfg.Name = name
	cb = New(cfg)
	r.breakers[name] = cb
	return cb
}

// GetWithConfig returns the circuit breaker registered under name,
// creating one with cfg if it does not exist. If the breaker already
// exists, the existing instance is returned and cfg is ignored.
func (r *Registry) GetWithConfig(name string, cfg Config) *CircuitBreaker {
	r.mu.RLock()
	cb, ok := r.breakers[name]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok = r.breakers[name]; ok {
		return cb
	}

	cfg.Name = name
	cb = New(cfg)
	r.breakers[name] = cb
	return cb
}

// All returns a snapshot of all registered circuit breakers keyed by name.
func (r *Registry) All() map[string]*CircuitBreaker {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*CircuitBreaker, len(r.breakers))
	for k, v := range r.breakers {
		out[k] = v
	}
	return out
}
