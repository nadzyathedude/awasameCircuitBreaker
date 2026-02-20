package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	cb "github.com/awasame/circuitbreaker"
)

// serviceConfig holds per-service failure probability, adjustable at runtime.
type serviceConfig struct {
	mu       sync.RWMutex
	failRate float64 // 0.0–1.0
}

func (sc *serviceConfig) FailRate() float64 {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.failRate
}

func (sc *serviceConfig) SetFailRate(rate float64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.failRate = rate
}

var (
	serviceA = &serviceConfig{failRate: 0.3}
	serviceB = &serviceConfig{failRate: 0.7}
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	registry := cb.NewRegistry(cb.Config{
		WindowSize:       20,
		FailureThreshold: 0.5,
		MinRequests:      5,
		RecoveryTimeout:  10 * time.Second,
		ProbeCount:       3,
		OnStateChange: func(name string, from, to cb.State) {
			slog.Warn("circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	})

	// Pre-create breakers so they appear in /api/status immediately.
	registry.Get("service-a")
	registry.Get("service-b")

	mux := http.NewServeMux()

	// GET /api/call?service=service-a
	mux.HandleFunc("/api/call", func(w http.ResponseWriter, r *http.Request) {
		svcName := r.URL.Query().Get("service")
		if svcName == "" {
			svcName = "service-a"
		}

		var svc *serviceConfig
		switch svcName {
		case "service-a":
			svc = serviceA
		case "service-b":
			svc = serviceB
		default:
			http.Error(w, fmt.Sprintf("unknown service: %s", svcName), http.StatusBadRequest)
			return
		}

		breaker := registry.Get(svcName)

		result, err := cb.Execute[string](breaker, r.Context(), func(ctx context.Context) (string, error) {
			return callUnstableService(ctx, svcName, svc.FailRate())
		})

		w.Header().Set("Content-Type", "application/json")

		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   err.Error(),
				"service": svcName,
				"state":   breaker.State().String(),
			})
			return
		}

		json.NewEncoder(w).Encode(map[string]string{
			"result":  result,
			"service": svcName,
			"state":   breaker.State().String(),
		})
	})

	// GET /api/status
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		all := registry.All()
		status := make(map[string]any, len(all))

		for name, breaker := range all {
			m := breaker.Metrics()
			status[name] = map[string]any{
				"state":              m.CurrentState.String(),
				"total_requests":     m.TotalRequests,
				"total_successes":    m.TotalSuccesses,
				"total_failures":     m.TotalFailures,
				"window_failure_rate": m.WindowFailureRate,
				"last_state_change":  m.LastStateChange.Format(time.RFC3339),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// POST /api/config  {"service": "service-a", "fail_rate": 0.8}
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Service  string  `json:"service"`
			FailRate float64 `json:"fail_rate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		var svc *serviceConfig
		switch body.Service {
		case "service-a":
			svc = serviceA
		case "service-b":
			svc = serviceB
		default:
			http.Error(w, fmt.Sprintf("unknown service: %s", body.Service), http.StatusBadRequest)
			return
		}

		svc.SetFailRate(body.FailRate)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"service":   body.Service,
			"fail_rate": body.FailRate,
		})
	})

	addr := ":8080"
	slog.Info("demo server starting", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// callUnstableService simulates an unreliable downstream service.
func callUnstableService(_ context.Context, name string, failRate float64) (string, error) {
	// Simulate latency.
	time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)

	if rand.Float64() < failRate {
		return "", fmt.Errorf("%s: internal server error", name)
	}
	return fmt.Sprintf("response from %s at %s", name, time.Now().Format(time.RFC3339Nano)), nil
}
