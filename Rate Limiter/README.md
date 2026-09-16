# Rate Limiter

A production-grade, modular rate limiting engine written in Go. Supports five algorithms, swappable storage backends (memory, Redis), adaptive health-based adjustment, Prometheus metrics, and a full HTTP service with a universal client.

---

## Quick Start

```go
package main
import (
    "fmt"
    "time"
    limiter "rate-limiter/internal/limiter"
    rate "rate-limiter/internal/storage"
)
func main() {
    storage := rate.NewMemoryStorage()
    l := limiter.NewFixedWindowWithLimit(storage, 100)
    policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
    result, _ := l.Check("alice", policy)
    fmt.Println("Allowed:", result.Allowed)
}
```

---

## Algorithm Comparison

| Algorithm | Best For | Trade-offs | Uses Storage |
|-----------|----------|------------|-------------|
| **Fixed Window** | Simple, predictable limits | Boundary burst (reset at window edge) | Yes |
| **Sliding Window Counter** | Smooth counting, no boundary burst | Approximate (counter blend) | Yes |
| **Sliding Window Log** | Precise per-request tracking | Memory-heavy (stores timestamps) | Yes |
| **Token Bucket** | Burst-friendly, network traffic | Complex implementation | Yes |
| **Leaky Bucket** | Smooth request flow (constant rate) | Rejects before filling | Yes |

**Choose Fixed Window** for simple per-period limits (e.g., 100 requests/minute).
**Choose Sliding Window Counter** for smoother counting without boundary bursts.
**Choose Sliding Window Log** for exact request history (audit trails, analytics).
**Choose Token Bucket** when you want to allow occasional bursts (APIs, webhooks).
**Choose Leaky Bucket** when you need strict constant-rate smoothing (queue-like).

---

## Installation

### Go Module

```bash
go get rate-limiter/internal/limiter
```

### Build Binary

```bash
go build -o rate-limiter ./cmd/rate-limiter
```

### Docker

```dockerfile
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o rate-limiter ./cmd/rate-limiter
FROM alpine:latest
COPY --from=builder /app/rate-limiter /usr/local/bin/
CMD ["rate-limiter"]
```

```bash
docker build -t rate-limiter .
docker run -p 8080:8080 rate-limiter
```

---

## Configuration

### Algorithm Selection (HTTP Service)

Send `algorithm` in the `/check` request body:

```json
{
  "identity": "user-123",
  "algorithm": "fixed_window",
  "policy": {
    "limit": 100,
    "window_seconds": 60
  }
}
```

**Supported algorithms:** `fixed_window`, `sliding_window_counter`, `sliding_window_log`, `token_bucket`, `leaky_bucket`

### Redis Configuration

```go
storage := rate.NewRedisStorage("localhost:6379", "password", 0)
l := limiter.NewFixedWindowWithLimit(storage, 100)
```

### Adjuster Configuration

```go
config := adjuster.AdjusterConfig{
    MetricsToWatch:   []string{"ErrorRate", "ResponseTime"},
    StableDuration:   5 * time.Minute,
    Interval:         30 * time.Second,
    MinLimit:         10,
    MaxLimit:         1000,
    Mode:             "conservative",
}
```

---

## HTTP API

Start the service: `./rate-limiter` (listens on `:8080`)

### POST /check

Check if a request is allowed.

**Request:**
```json
{
  "identity": "alice",
  "algorithm": "fixed_window",
  "policy": { "limit": 10, "window_seconds": 60 }
}
```

**Response:**
```json
{
  "allowed": true,
  "limit": 10,
  "remaining": 9,
  "retry_after": 0,
  "reset_time": "2026-09-16T08:00:00Z"
}
```

**Response Headers:**
| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Total limit for this identity |
| `X-RateLimit-Remaining` | Remaining requests in window |
| `X-RateLimit-Reset` | Window reset time (RFC3339) |
| `Retry-After` | Seconds to wait (only when denied) |

### POST /limit

Update limit for an algorithm (admin).

**Request:**
```json
{ "algorithm": "fixed_window", "limit": 200 }
```

**Response:**
```json
{ "status": "limit updated" }
```

### GET /metrics

Prometheus-format metrics. Scrape with Prometheus or add to `/prometheus` in your monitoring.

### GET /health

Health check for load balancers.

**Response:**
```json
{ "status": "ok" }
```

---

## Metrics / Prometheus

The HTTP service exposes Prometheus metrics at `GET /metrics`.

Example output:
```
# HELP rate_limiter_requests_allowed Total allowed requests per identity
# TYPE rate_limiter_requests_allowed counter
rate_limiter_requests_allowed{identity="alice"} 42
# HELP rate_limiter_current_limit Current limit per algorithm
# TYPE rate_limiter_current_limit gauge
rate_limiter_current_limit{algorithm="fixed_window"} 100
# HELP rate_limiter_checks_total Total number of checks
# TYPE rate_limiter_checks_total counter
rate_limiter_checks_total 1000
```

**In your Go app:** Use `metrics.Wrap()` to wrap limiters and automatically collect metrics:

```go
collector := metrics.NewCollector()
l := metrics.Wrap(limiter.NewFixedWindowWithLimit(storage, 100), collector, "fixed_window")
// All checks through l will be recorded in collector
```

---

## Adaptive Limiting (Adjuster)

The Adjuster automatically tunes limits based on system health:

```go
provider := &adjuster.BasicMetrics{ErrorRate: 0.01, ResponseTime: 200 * time.Millisecond}
config := adjuster.AdjusterConfig{
    MetricsToWatch: []string{"ErrorRate", "ResponseTime"},
    StableDuration: 5 * time.Minute,
}
a, _ := adjuster.NewAdjuster([]limiter.LimitSetter{l}, provider, config)
a.Start()
defer a.Stop()
```

**How it works:**
1. Fetches metrics every `Interval` (default 30s)
2. Calculates health score (0-100) from configured metrics
3. Increases limit by 10% when health > 70
4. Decreases limit by 10-50% when health < 30
5. **Hysteresis:** No adjustment within `StableDuration` (default 5 min)
6. **Circuit Breaker:** Reverts limit and extends cooldown to 15 min if adjustment worsens health

---

## HTTP Client (Non-Go)

Use `pkg/rateclient` from any language via HTTP:

```go
client := rateclient.New("http://localhost:8080")
result, err := client.Check("alice", "fixed_window", limiter.Policy{Limit: 100, Window: 60 * time.Second})
fmt.Println("Allowed:", result.Allowed)
```

---

## Project Structure

```
rate-limiter/
├── internal/              # Library (importable by Go devs)
│   ├── limiter/           # 5 algorithms + interfaces
│   ├── storage/           # Memory + Redis backends
│   ├── adjuster/          # Health scoring + auto-adjustment
│   └── metrics/           # Prometheus collector + wrapper
├── cmd/rate-limiter/      # HTTP service entry point
├── pkg/rateclient/         # HTTP client (any language)
├── tests/integration/      # Integration tests
├── docs/                   # Documentation
└── fixes.md               # Known issues and planned fixes
```

## Contributing

1. Pick a fix from `fixes.md` (prioritized P0 → P2)
2. Create branch: `git checkout -b fix/<name>`
3. Implement + test
4. Commit: `git commit -m "fix: <description>"`
5. Push → Open PR → Review → Merge

Each PR is a learning unit. See `fixes.md` for the full list of 13 planned fixes.

---

## License

MIT
