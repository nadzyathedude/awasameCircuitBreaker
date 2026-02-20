# circuitbreaker

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/nadzyathedude/awasameCircuitBreaker)](https://goreportcard.com/report/github.com/nadzyathedude/awasameCircuitBreaker)
[![Coverage](https://img.shields.io/badge/coverage-97.1%25-brightgreen)](https://github.com/nadzyathedude/awasameCircuitBreaker)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-zero-success)](go.mod)
[![GoDoc](https://pkg.go.dev/badge/github.com/nadzyathedude/awasameCircuitBreaker.svg)](https://pkg.go.dev/github.com/nadzyathedude/awasameCircuitBreaker)

Go-библиотека, реализующая паттерн **Circuit Breaker** для защиты вызовов к нестабильным сервисам. Zero dependencies, thread-safe, generics-first API.

## Architecture

```
                         ┌─────────────────────────────────────────────────┐
                         │              Circuit Breaker FSM               │
                         │                                                │
                         │                                                │
    ┌────────────────────┼────────────────────────────────────────────┐   │
    │                    │                                            │   │
    │         ┌──────────▼──────────┐                                 │   │
    │         │                     │                                 │   │
    │         │    CLOSED           │                                 │   │
    │         │                     │                                 │   │
    │         │  Requests pass      │                                 │   │
    │         │  through. Outcomes  │                                 │   │
    │         │  recorded in        │                                 │   │
    │         │  sliding window.    │                                 │   │
    │         │                     │                                 │   │
    │         └──────────┬──────────┘                                 │   │
    │                    │                                            │   │
    │                    │ failure rate >= threshold                  │   │
    │                    │ AND count >= MinRequests                   │   │
    │                    │                                            │   │
    │         ┌──────────▼──────────┐                                 │   │
    │         │                     │                                 │   │
    │         │    OPEN             │                                 │   │
    │         │                     │                                 │   │
    │         │  All requests       │                                 │   │
    │         │  rejected with      │                                 │   │
    │         │  ErrCircuitOpen     │                                 │   │
    │         │  (or fallback).     │                                 │   │
    │         │                     │                                 │   │
    │         └──────────┬──────────┘                                 │   │
    │                    │                                            │   │
    │                    │ RecoveryTimeout elapsed                    │   │
    │                    │                                            │   │
    │         ┌──────────▼──────────┐                                 │   │
    │         │                     │  probe failed                   │   │
    │         │    HALF-OPEN        ├─────────────────────────────────┘   │
    │         │                     │  ──► back to OPEN                   │
    │         │  Allow ProbeCount   │                                     │
    │         │  trial requests.    │                                     │
    │         │                     │                                     │
    │         └──────────┬──────────┘                                     │
    │                    │                                                │
    │                    │ all ProbeCount probes succeed                  │
    └────────────────────┘                                                │
               ──► back to CLOSED                                        │
               window reset                                              │
                                                                         │
                         └─────────────────────────────────────────────────┘
```

### Sliding Window (Ring Buffer)

```
  Window size = 8, MinRequests = 5, Threshold = 0.5

  ┌───┬───┬───┬───┬───┬───┬───┬───┐
  │ ✓ │ ✗ │ ✓ │ ✗ │ ✗ │ ✓ │   │   │   count = 6, fails = 3
  └───┴───┴───┴───┴───┴───┴───┴───┘   failure rate = 3/6 = 0.50
                              ▲
                              │
                             pos (next write)

  New outcome overwrites oldest slot when buffer is full.
  O(1) record, O(1) failure rate — no re-scanning.
```

### Per-Endpoint Breakers via Registry

```
  ┌──────────────────────────────────────────────┐
  │                  Registry                     │
  │               (sync.RWMutex)                  │
  │                                               │
  │   "payment-api"  ──► CircuitBreaker (closed)  │
  │   "user-api"     ──► CircuitBreaker (open)    │
  │   "search-api"   ──► CircuitBreaker (closed)  │
  │                                               │
  │   Get("name")  → find or create               │
  │   All()        → snapshot of all breakers     │
  └──────────────────────────────────────────────┘
```

## Features

- **Three-state FSM** — Closed / Open / Half-Open with configurable transitions
- **Sliding window** — Ring buffer, O(1) per operation, no allocations after init
- **Generics** — Type-safe `Execute[T]` wrapper (Go 1.18+)
- **Registry** — Per-endpoint breakers with thread-safe lookup/creation
- **Fallback** — Optional fallback when circuit is open
- **Context-aware** — `context.Context` cancellation is not counted as a failure
- **Callbacks** — `OnStateChange` hook for monitoring/alerting
- **Logging** — State transitions logged via `slog` (Go 1.21+)
- **Thread-safe** — Passes `go test -race`, safe for concurrent use
- **Zero dependencies** — Standard library only

## Installation

```bash
go get github.com/nadzyathedude/awasameCircuitBreaker
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "time"

    cb "github.com/nadzyathedude/awasameCircuitBreaker"
)

func main() {
    breaker := cb.New(cb.Config{
        Name:             "my-service",
        WindowSize:       20,
        FailureThreshold: 0.5,
        MinRequests:      5,
        RecoveryTimeout:  30 * time.Second,
        ProbeCount:       3,
    })

    result, err := cb.Execute[string](breaker, context.Background(),
        func(ctx context.Context) (string, error) {
            // Your call to an external service
            return "data", nil
        },
    )
    if err != nil {
        fmt.Println("error:", err)
        return
    }
    fmt.Println("result:", result)
}
```

## Registry — Multiple Services

```go
registry := cb.NewRegistry(cb.Config{
    FailureThreshold: 0.5,
    RecoveryTimeout:  30 * time.Second,
})

// Each endpoint gets its own breaker, created on first access
result, err := cb.Execute[PaymentResponse](
    registry.Get("payment-service"),
    ctx,
    callPaymentService,
)
```

## Fallback

```go
breaker := cb.New(cb.Config{
    Name: "with-fallback",
    Fallback: func(ctx context.Context, err error) (any, error) {
        // Return cached/default data when circuit is open
        return cachedResponse, nil
    },
})
```

## State Change Monitoring

```go
breaker := cb.New(cb.Config{
    Name: "monitored",
    OnStateChange: func(name string, from, to cb.State) {
        metrics.RecordStateChange(name, from.String(), to.String())
        if to == cb.StateOpen {
            alerting.Notify(fmt.Sprintf("Circuit %s opened!", name))
        }
    },
})
```

## Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `Name` | `""` | Breaker name for logs and metrics |
| `WindowSize` | `20` | Sliding window capacity (ring buffer size) |
| `FailureThreshold` | `0.5` | Failure ratio (0.0–1.0) to trip the breaker |
| `MinRequests` | `5` | Minimum outcomes in window before breaker can trip |
| `RecoveryTimeout` | `30s` | Duration in Open state before transitioning to Half-Open |
| `ProbeCount` | `3` | Successful probes required in Half-Open to close |
| `Fallback` | `nil` | Called instead of returning `ErrCircuitOpen` |
| `OnStateChange` | `nil` | Callback fired on every state transition |

## API Reference

```go
// Create a breaker
func New(cfg Config) *CircuitBreaker

// Execute through the breaker (untyped)
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) (any, error)) (any, error)

// Execute through the breaker (generic, type-safe)
func Execute[T any](cb *CircuitBreaker, ctx context.Context, fn func(ctx context.Context) (T, error)) (T, error)

// Inspect state and metrics
func (cb *CircuitBreaker) State() State
func (cb *CircuitBreaker) Metrics() Metrics

// Registry for per-endpoint breakers
func NewRegistry(defaultConfig Config) *Registry
func (r *Registry) Get(name string) *CircuitBreaker
func (r *Registry) GetWithConfig(name string, cfg Config) *CircuitBreaker
func (r *Registry) All() map[string]*CircuitBreaker
```

## Project Structure

```
circuitbreaker/
├── breaker.go          CircuitBreaker, Config, Execute, Execute[T]
├── state.go            State enum and transitions
├── window.go           Sliding window (ring buffer)
├── registry.go         Thread-safe Registry for per-endpoint breakers
├── errors.go           ErrCircuitOpen
├── metrics.go          Metrics struct
├── breaker_test.go     17 test cases (state transitions, fallback, concurrency, generics)
├── window_test.go       9 test cases (ring buffer correctness, edge cases)
├── registry_test.go     9 test cases (CRUD, concurrency)
└── example/demo/
    ├── main.go         Demo HTTP service with two unstable backends
    └── README.md       Run instructions with curl examples
```

## Testing

```bash
# Run all tests with race detector and coverage
go test -race -cover ./...

# Verbose output
go test -race -cover -v ./...
```

**35 tests, 97.1% coverage**, passes `-race` cleanly.

## Demo Service

A live HTTP demo is included in `example/demo/`. It simulates two unstable services with configurable failure rates and shows the circuit breaker in action.

```bash
go run example/demo/main.go
```

```bash
# Hit the unstable service until the breaker trips
for i in $(seq 1 20); do
  curl -s "localhost:8080/api/call?service=service-b"
  echo
done

# Check breaker states
curl -s localhost:8080/api/status | jq .

# Make the service stable and watch the breaker recover
curl -s -X POST localhost:8080/api/config \
  -H "Content-Type: application/json" \
  -d '{"service": "service-b", "fail_rate": 0.0}'
```

See [example/demo/README.md](example/demo/README.md) for the full walkthrough.

## License

MIT
