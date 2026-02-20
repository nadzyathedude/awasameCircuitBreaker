# Circuit Breaker Demo

HTTP-сервис, демонстрирующий работу circuit breaker в реальном времени.

## Запуск

```bash
cd example/demo
go run main.go
```

Сервер стартует на `http://localhost:8080`.

## Эндпоинты

### GET /api/call?service=<name>

Делает вызов через circuit breaker к mock-сервису.

- `service-a` — вероятность ошибки 30% (по умолчанию)
- `service-b` — вероятность ошибки 70% (по умолчанию)

### GET /api/status

Возвращает JSON с текущим состоянием и метриками всех breaker'ов.

### POST /api/config

Меняет вероятность ошибки сервиса на лету.

```json
{"service": "service-a", "fail_rate": 0.9}
```

## Примеры curl-команд

### 1. Проверить статус всех breaker'ов

```bash
curl -s localhost:8080/api/status | jq .
```

Ожидаемый вывод (начальное состояние):
```json
{
  "service-a": {
    "state": "closed",
    "total_requests": 0,
    "total_successes": 0,
    "total_failures": 0,
    "window_failure_rate": 0,
    "last_state_change": "2024-01-01T00:00:00Z"
  },
  "service-b": {
    "state": "closed",
    ...
  }
}
```

### 2. Вызвать сервис A (стабильный, 30% ошибок)

```bash
# Одиночный вызов
curl -s localhost:8080/api/call?service=service-a | jq .

# 20 вызовов подряд — breaker скорее всего останется closed
for i in $(seq 1 20); do
  curl -s localhost:8080/api/call?service=service-a | jq -c .
done
```

### 3. Вызвать сервис B (нестабильный, 70% ошибок)

```bash
# 20 вызовов — breaker должен перейти в Open
for i in $(seq 1 20); do
  curl -s localhost:8080/api/call?service=service-b | jq -c .
done
```

Ожидаемый вывод: первые ~5 запросов проходят (или возвращают ошибку сервиса), затем breaker открывается:
```json
{"error":"circuit breaker is open","service":"service-b","state":"open"}
```

### 4. Подождать восстановления (10 секунд) и повторить

```bash
sleep 12
curl -s localhost:8080/api/call?service=service-b | jq .
```

Breaker перейдёт в `half-open` и пропустит пробный запрос.

### 5. Изменить вероятность ошибок на лету

```bash
# Сделать service-b стабильным
curl -s -X POST localhost:8080/api/config \
  -H "Content-Type: application/json" \
  -d '{"service": "service-b", "fail_rate": 0.0}' | jq .

# Подождать восстановления и убедиться что breaker закроется
sleep 12
for i in $(seq 1 5); do
  curl -s localhost:8080/api/call?service=service-b | jq -c .
done
```

### 6. Полный сценарий

```bash
# 1. Начальный статус
curl -s localhost:8080/api/status | jq .

# 2. Нагрузить service-b до срабатывания breaker'а
for i in $(seq 1 30); do
  curl -s localhost:8080/api/call?service=service-b | jq -c .
done

# 3. Проверить что breaker открыт
curl -s localhost:8080/api/status | jq '.["service-b"].state'

# 4. Сделать сервис стабильным
curl -s -X POST localhost:8080/api/config \
  -d '{"service": "service-b", "fail_rate": 0.0}' \
  -H "Content-Type: application/json"

# 5. Подождать Recovery Timeout
sleep 12

# 6. Отправить probe-запросы
for i in $(seq 1 5); do
  curl -s localhost:8080/api/call?service=service-b | jq -c .
done

# 7. Убедиться что breaker закрылся
curl -s localhost:8080/api/status | jq '.["service-b"].state'
```

## Логи

В stdout отображаются смены состояний breaker'ов:

```
time=... level=WARN msg="circuit breaker state change" name=service-b from=closed to=open
time=... level=WARN msg="circuit breaker state change" name=service-b from=open to=half-open
time=... level=WARN msg="circuit breaker state change" name=service-b from=half-open to=closed
```
