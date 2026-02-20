# circuitbreaker

Go-библиотека, реализующая паттерн Circuit Breaker для защиты вызовов к нестабильным сервисам.

## Возможности

- Три состояния: Closed → Open → Half-Open → Closed
- Sliding window на кольцевом буфере для подсчёта ошибок
- Настраиваемый порог ошибок, размер окна, таймаут восстановления
- Generic-функция `Execute[T]` для типобезопасных вызовов
- `Registry` для управления per-endpoint breaker'ами
- Fallback при Open state
- Callback на смену состояний
- Context-aware: отмена контекста не считается ошибкой
- Логирование через `slog`
- Потокобезопасность, zero dependencies

## Установка

```bash
go get github.com/awasame/circuitbreaker
```

## Быстрый старт

```go
package main

import (
    "context"
    "fmt"
    "time"

    cb "github.com/awasame/circuitbreaker"
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
            // Ваш вызов к внешнему сервису
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

## Registry для нескольких сервисов

```go
registry := cb.NewRegistry(cb.Config{
    FailureThreshold: 0.5,
    RecoveryTimeout:  30 * time.Second,
})

// Каждый endpoint получает свой breaker
result, err := cb.Execute[string](
    registry.Get("payment-service"),
    ctx,
    callPaymentService,
)
```

## Конфигурация

| Параметр | По умолчанию | Описание |
|----------|-------------|----------|
| `WindowSize` | 20 | Размер скользящего окна |
| `FailureThreshold` | 0.5 | Порог ошибок (0.0–1.0) для перехода в Open |
| `MinRequests` | 5 | Минимум запросов в окне до срабатывания |
| `RecoveryTimeout` | 30s | Время в Open перед переходом в Half-Open |
| `ProbeCount` | 3 | Число успешных проб для возврата в Closed |

## Demo

См. [example/demo/README.md](example/demo/README.md) для запуска демо HTTP-сервиса.

## Тесты

```bash
go test -race -cover ./...
```
