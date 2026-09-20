# New Additions — Production Readiness Fixes

Each fix below explains what was broken, why it matters, what cannot be done without it, and the technical decisions behind it. Code blocks have English comments at the block level (not line-by-line — that's for the codebase itself).

---

## Fix 1: Grafana Dashboards + Observability Stack

The Prometheus metrics exist at `/metrics` but there's no way to visualize them. No dashboards, no alerting rules, no Prometheus scrape config, no docker-compose stack.

What cannot be done without it: real-time monitoring, automated alerting, historical trend analysis, team-wide shared dashboards, incident triage.

**Code blocks:**

```yaml
# docker-compose.yml — Prometheus + Grafana stack
# Prometheus scrapes /metrics every 15s from the rate limiter service
# Grafana reads Prometheus as a data source and renders dashboards
version: '3.8'
services:
  prometheus:
    image: prom/prometheus:latest
    ports: ["9090:9090"]
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus

  grafana:
    image: grafana/grafana:latest
    ports: ["3000:3000"]
    depends_on: [prometheus]
    environment:
      GF_SECURITY_ADMIN_PASSWORD: admin
    volumes:
      - grafana-data:/var/lib/grafana

volumes:
  prometheus-data:
  grafana-data:
```

```yaml
# prometheus.yml — scrape config
# Points Prometheus at the rate limiter's /metrics endpoint
# 15s interval matches the check frequency
scrape_configs:
  - job_name: 'rate-limiter'
    static_configs:
      - targets: ['rate-limiter:8080']
    scrape_interval: 15s
```

```jsonc
// grafana-dashboard.json — panels for key metrics
// Shows allowed/denied request rates, current limits, check latency, and health
// Alert rules trigger when denial rate exceeds threshold or service is down
{
  "title": "Rate Limiter Dashboard",
  "panels": [
    {"title": "Requests Allowed", "type": "timeseries"},
    {"title": "Requests Denied", "type": "timeseries"},
    {"title": "Current Limits", "type": "stat"},
    {"title": "Check Duration", "type": "timeseries"},
    {"title": "Health Status", "type": "stat"}
  ]
}
```

**Technical & engineering decisions:**
- Prometheus over custom solutions: standard ecosystem, native Grafana integration, query language (PromQL)
- 15s scrape interval: matches typical rate limiter check frequency, balances granularity vs overhead
- Grafana dashboards as JSON: version-controllable, shareable, reproducible across environments
- Alert rules separate from dashboards: alerts work independently of visualization, can trigger external notifications

---

## Fix 2: Official Prometheus Client

Currently uses a custom text-format collector that manually builds Prometheus exposition. Works, but reinvents metric types, thread safety, and Go runtime metrics.

What cannot be done without it: Go runtime metrics (GC, heap, goroutines), histogram bucket calculations, standard tooling integration, future Prometheus format updates.

**Code blocks:**

```go
// metrics/collector.go — uses official prometheus/client_golang
// Counter: monotonically increasing value (request counts)
// Histogram: observed values in buckets (check duration)
// Gauge: value that goes up and down (current limit)
import "github.com/prometheus/client_golang/prometheus"

// Wrap instruments any limiter with Prometheus metrics
// Returns a limiter that records duration, allowed count, denied count
// to the global Prometheus registry automatically
func Wrap(l Limiter, registry prometheus.Registerer) Limiter {
    // Creates CounterVec for allowed/denied by algorithm type
    // Creates Histogram for check duration with buckets
    // Wraps Check() to record metrics before/after each call
}
```

**Technical & engineering decisions:**
- `prometheus/client_golang` over custom: battle-tested, maintained by Prometheus team, no format drift risk
- Counter for allowed/denied: monotonically increasing, correct Prometheus semantics (never decreases)
- Histogram for duration: captures distribution, not just average — enables latency SLA queries
- Global registry: simplicity over isolation; service has one set of metrics anyway

---

## Fix 3: Graceful Shutdown

`main.go` uses `log.Fatal(http.ListenAndServe(...))` which calls `os.Exit(1)` on any error. SIGTERM/SIGINT from Kubernetes, Docker, or systemd cause immediate termination — all in-flight requests are dropped.

What cannot be done without it: zero-downtime deployments, connection draining, metrics flushing, clean connection close, client trust during rolling updates.

**Code blocks:**

```go
// main.go — graceful shutdown with signal handling
// Waits for SIGTERM or SIGINT, then stops accepting new connections
// and finishes all in-flight requests before exiting

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
    defer stop()

    srv := &http.Server{
        Addr: ":8080",
        Handler: router(),
        // ShutdownTimeout: max time to wait for requests to finish
        ShutdownTimeout: 30 * time.Second,
    }

    // Run server in goroutine so we can wait for shutdown signal
    go func() {
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    // Block until shutdown signal received
    <-ctx.Done()

    // Graceful shutdown: stop accepting new connections, wait for active requests
    ctxShutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    srv.Shutdown(ctxShutdown)
}
```

```go
// middleware that tracks active requests
// Increments counter when request starts, decrements when it ends
// Shutdown waits for this counter to reach zero (or timeout)
func TrackRequests(h http.Handler) http.Handler {
    var wg sync.WaitGroup
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        wg.Add(1)
        defer wg.Done()
        h.ServeHTTP(w, r)
    })
}
```

**Technical & engineering decisions:**
- `signal.NotifyContext` over `signal.Notify(chan os.Signal)`: context-based, integrates with request cancellation
- 30s shutdown timeout: long enough for normal requests, short enough to not hang forever
- `http.Server.Shutdown` over `os.Exit`: completes in-flight requests, closes listeners cleanly
- WaitGroup in middleware: allows shutdown to know when all requests are done (alternative to server's built-in tracking)

---

## Fix 4: HTTPS/TLS

Service runs HTTP only. No TLS, no certificate management, no HTTP→HTTPS redirect. Identity strings, rate limit data, and infrastructure details are transmitted in plaintext.

What cannot be done without it: secure API access, compliance (PCI-DSS, HIPAA, SOC 2), trust with reverse proxies, protection against MITM attacks.

**Code blocks:**

```go
// main.go — TLS support
// Loads certificate/key pair and starts HTTPS server
// Falls back to HTTP when TLS not configured (development mode)

func main() {
    server := &http.Server{Addr: ":8080", Handler: router()}

    if tlsCert != "" && tlsKey != "" {
        // HTTPS mode: TLS termination at this service
        log.Fatal(server.ListenAndServeTLS(tlsCert, tlsKey))
    } else {
        // HTTP mode: TLS handled by reverse proxy (nginx, ALB, CloudFlare)
        // Trust X-Forwarded-Proto header for scheme-aware redirects
        log.Fatal(server.ListenAndServe())
    }
}
```

```go
// redirect.go — HTTP to HTTPS redirect
// When TLS is enabled, all HTTP traffic redirects to HTTPS
// Status 301 (permanent) — clients cache this, future requests use HTTPS
func HTTPSRedirect(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("X-Forwarded-Proto") != "https" {
        target := "https://" + r.Host + r.RequestURI
        http.Redirect(w, r, target, http.StatusMovedPermanently)
    }
}
```

**Technical & engineering decisions:**
- Dual mode (HTTPS + HTTP fallback): production uses TLS, development doesn't need certs
- Reverse proxy TLS vs self-signed: production should use a real proxy (nginx, ALB), not self-signed certs in the app
- 301 redirect: permanent, clients cache it — reduces HTTP traffic over time
- `X-Forwarded-Proto`: when behind a proxy, the proxy sets this header to indicate the original scheme

---

## Fix 5: Request Timeouts on `/check`

The `/check` handler makes HTTP calls to storage (Redis via HTTP proxy). If storage is slow or unresponsive, the request hangs indefinitely. This leaks goroutines and can crash the service.

What cannot be done without it: cascading failure prevention, goroutine safety, predictable latency, circuit breaker functionality, graceful degradation.

**Code blocks:**

```go
// handlers/check.go — request timeout on storage call
// Every /check request gets a 2-second context timeout
// If storage doesn't respond in time, the request fails gracefully
// Fail-open: allow the request rather than denying all traffic

func CheckHandler(w http.ResponseWriter, r *http.Request) {
    // Create context with 2s timeout for storage call
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()

    // Pass context to storage client
    // If timeout, return Allowed:true (fail open) — rate limiter shouldn't block all traffic
    result, err := storage.Check(ctx, identity, policy)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            // Storage timeout — fail open to avoid cascading failure
            return AllowedResult(true)
        }
        return ErrorResult(err)
    }
    return result
}
```

```go
// main.go — server-level timeouts
// ReadHeaderTimeout: max time to wait for request headers
// WriteTimeout: max time for response write (includes handler execution)
// IdleTimeout: max time between keepalive connections
srv := &http.Server{
    ReadHeaderTimeout: 5 * time.Second,
    WriteTimeout:      10 * time.Second,
    IdleTimeout:       60 * time.Second,
}
```

**Technical & engineering decisions:**
- 2s storage timeout: short enough to detect slow storage, long enough for normal Redis (typically <10ms)
- Fail-open on timeout: denying all traffic when storage is slow is worse than allowing some extra traffic temporarily
- Fail-closed on error: non-timeout errors (bad request, invalid policy) should still deny
- Server-level timeouts separate from request-level: server timeouts catch slow clients, request timeouts catch slow storage

---

## Fix 6: pprof Endpoint

No CPU/memory profiling available. When production performance issues occur, there's no built-in diagnostic tool.

What cannot be done without it: performance optimization, memory leak detection, goroutine leak detection, production debugging, capacity planning.

**Code blocks:**

```go
// main.go — pprof registration (disabled by default)
// Import net/http/pprof to register /debug/pprof/ endpoints
// Protected by --enable-pprof flag and optional basic auth

import _ "net/http/pprof"

func main() {
    router := mux.NewRouter()

    // Health check and metrics are public
    router.HandleFunc("/health", healthHandler)
    router.HandleFunc("/metrics", metricsHandler)

    // pprof only when explicitly enabled
    if enablePprof {
        // Optional basic auth for pprof endpoints
        // CPU profiling, heap profiling, goroutine count available at /debug/pprof/
        router.PathPrefix("/debug/pprof").Handler(
            basicAuth(http.DefaultServeMux, pprofUser, pprofPass),
        )
    }
}
```

**Technical & engineering decisions:**
- Disabled by default: pprof endpoints expose internal state — must be opt-in
- Basic auth: prevents unauthorized profiling access (memory contents, CPU data)
- `_` import: registers handlers on `http.DefaultServeMux` — standard Go pattern
- Flag-based toggle: `--enable-pprof` allows enabling in production when needed without code changes

---

## Fix 7: Distributed Tracing

No Jaeger, Zipkin, or OpenTelemetry instrumentation. Cannot trace request paths, measure per-hop latency, or identify where delays occur.

What cannot be done without it: latency attribution, error tracing, dependency analysis, SLA monitoring, cascading failure diagnosis.

**Code blocks:**

```go
// main.go — OpenTelemetry setup
// Creates a tracer for the rate-limiter service
// Exporter sends traces to collector (configured via env vars)
// Sampling: 100% in development, configurable percentage in production

import "go.opentelemetry.io/otel"
import "go.opentelemetry.io/otel/exporters/otlp/otlptrace"

func initTracer() func() {
    // Configure exporter (OTLP/HTTP or OTLP/gRPC)
    // OTEL_EXPORTER_OTLP_ENDPOINT env var controls collector address
    // AlwaysFromSample with 1.0 = 100% sampling in dev
    // TailSampling in production for cost control
}
```

```go
// handlers/check.go — span creation in /check
// Parent span for the HTTP request
// Child span for the storage call (shows storage latency separately)
// Trace context propagates to storage via headers (X-Trace-Id)

func CheckHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Start parent span for this request
    ctx, span := tracer.Start(ctx, "check")
    defer span.End()

    // Set attributes on the span (identity, algorithm, policy)
    span.SetAttribute("identity", identity)
    span.SetAttribute("algorithm", algorithm)

    // Storage call as child span — shows how long storage took
    ctx, storageSpan := tracer.Start(ctx, "storage.check")
    result, err := storage.Check(ctx, identity, policy)
    storageSpan.End()

    // Record success/failure on span
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    } else {
        span.SetStatus(codes.Ok, "allowed")
    }
}
```

**Technical & engineering decisions:**
- OpenTelemetry over Jaeger/Zipkin direct: OTel is the unified standard, supports both backends
- Parent + child spans: shows the full request path (client → rate limiter → storage → response)
- Env var configuration: tracing backend can change without code changes
- 100% sampling in dev: see everything during development, reduce in production for cost

---

## Fix 8: Helm Charts and Kubernetes Manifests

Docker image exists but no Kubernetes deployment manifests, HPA, Service, ConfigMap, or Ingress. Cannot deploy to any Kubernetes cluster.

What cannot be done without it: container orchestration, auto-scaling, configuration management, service discovery, zero-downtime deployments.

**Code blocks:**

```yaml
# deploy/kubernetes/deployment.yaml — rate limiter pod spec
# Resource requests/limits prevent resource starvation
# Liveness probe: if /health fails 3 times in 30s, restart the pod
# Readiness probe: if /health fails, remove from service endpoints
# Rolling update: max 25% surge, 25% unavailable (zero downtime)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rate-limiter
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
  template:
    spec:
      containers:
      - name: rate-limiter
        image: clydeeshun94/rate-limiter:latest
        ports:
        - containerPort: 8080
        resources:
          requests: {cpu: "100m", memory: "128Mi"}
          limits: {cpu: "500m", memory: "512Mi"}
        livenessProbe:
          httpGet: {path: /health, port: 8080}
          initialDelaySeconds: 5
          periodSeconds: 10
          failureThreshold: 3
        readinessProbe:
          httpGet: {path: /health, port: 8080}
          initialDelaySeconds: 3
          periodSeconds: 5
```

```yaml
# deploy/kubernetes/hpa.yaml — auto-scaling
# Scales based on CPU usage (60% target) AND denial rate
# Min 3 replicas (high availability), max 20 (cost control)
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: rate-limiter-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rate-limiter
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target: {type: Utilization, averageUtilization: 60}
  - type: Pods
    pods:
      metric: {name: rate_limiter_requests_denied}
      target: {type: AverageValue, averageValue: "10"}
```

**Technical & engineering decisions:**
- 3 replicas minimum: high availability, tolerates 1 node failure
- Rolling update strategy: zero downtime during deployments
- HPA on CPU + denial rate: scales on load AND on error rate (defensive scaling)
- Liveness vs readiness: liveness restarts broken pods, readiness stops traffic to unhealthy pods
- Resource limits: prevents one pod from consuming all cluster resources

---

## Fix 9: GitHub Releases with Binary Artifacts

No tagged binary releases. Users must build from source or `go get` — requires Go toolchain on consumer's machine.

What cannot be done without it: one-click deployment, cross-platform support, version pinning, CI/CD integration, security auditing.

**Code blocks:**

```makefile
# Makefile — release targets
# Builds cross-platform binaries and creates GitHub Release

.PHONY: release
release: build-all
	gh release create $(VERSION) \
		--title "$(VERSION)" \
		--notes "Release $(VERSION)" \
		$(wildcard rate-limiter-*)

.PHONY: build-all
build-all:
	CGO_ENABLED=0 go build -o rate-limiter-linux-amd64 ./cmd/rate-limiter
	CGO_ENABLED=0 go build -o rate-limiter-linux-arm64 ./cmd/rate-limiter
	CGO_ENABLED=0 go build -o rate-limiter-darwin-amd64 ./cmd/rate-limiter
	CGO_ENABLED=0 go build -o rate-limiter-darwin-arm64 ./cmd/rate-limiter
	CGO_ENABLED=0 go build -o rate-limiter-windows-amd64.exe ./cmd/rate-limiter
```

```yaml
# .goreleaser.yaml — automated release config
# Triggers on git tag, builds all platforms, creates GitHub Release with checksums
releases:
  uses: goreleaser/goreleaser-action@v5
  with:
    version: latest
    args: release --clean
env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Technical & engineering decisions:**
- `CGO_ENABLED=0`: static binaries, no system dependencies, works on any Linux/macOS/Windows
- Git tag triggers release: version pinning, semantic versioning enforcement
- Checksums (SHA256): integrity verification, security auditing
- goreleaser: standard tool for Go releases, handles signing, changelogs, multiple formats

---

## Fix 10: Redis Sentinel and Cluster Support

Redis Storage connects to a single Redis instance. No Sentinel failover, no Cluster sharding, no connection pool config. If Redis fails, all rate limiting fails.

What cannot be done without it: high availability, horizontal scaling, geographic distribution, read scaling, disaster recovery.

**Code blocks:**

```go
// storage/redis_sentinel.go — Redis Sentinel storage
// Monitors master node, auto-failovers to replica on master failure
// Same Storage interface as RedisStorage — swapable

type RedisSentinelStorage struct {
    client *redis.FailoverClient
    ctx    context.Context
}

func NewRedisSentinelStorage(masterName string, sentinelAddrs []string, password string, db int) *RedisSentinelStorage {
    return &RedisSentinelStorage{
        client: redis.NewFailoverClient(&redis.FailoverOptions{
            MasterName:     masterName,
            SentinelAddrs:  sentinelAddrs,
            Password:       password,
            DB:             db,
            PoolSize:       100,          // connection pool size
            MinIdleConns:   10,           // keep 10 idle connections
            ConnMaxLifetime: 30 * time.Second,
        }),
        ctx: context.Background(),
    }
}

// Get/Set/Delete implement Storage interface using FailoverClient
// FailoverClient automatically redirects to current master
// If master fails, Sentinel promotes a replica and client follows
```

```go
// storage/redis_cluster.go — Redis Cluster storage
// Distributes keys across multiple Redis nodes automatically
// Client handles routing, failover, and redirection internally

type RedisClusterStorage struct {
    client *redis.ClusterClient
    ctx    context.Context
}

func NewRedisClusterStorage(addrs []string, password string, db int) *RedisClusterStorage {
    return &RedisClusterStorage{
        client: redis.NewClusterClient(&redis.ClusterOptions{
            Addrs:         addrs,
            Password:      password,
            PoolSize:      100,
            MinIdleConns:  10,
            ConnMaxLifetime: 30 * time.Second,
        }),
        ctx: context.Background(),
    }
}

// Get/Set/Delete implement Storage interface using ClusterClient
// ClusterClient routes keys to correct shard automatically
// Handles MOVED/ASK redirects, master failover within cluster
```

**Technical & engineering decisions:**
- Two separate types (Sentinel vs Cluster): different architectures, different failure modes
- Same `Storage` interface: algorithms don't know which Redis mode is used
- Connection pooling: prevents connection exhaustion under high concurrency
- JSON serialization: same as RedisStorage — `Record` struct with `Timestamps` marshals to JSON
- No TTL on Set: dev-configured via Redis CLI or middleware, not hardcoded

---

## Fix 11: Rate Limiting on /metrics and /health

Both endpoints are unthrottled and unauthenticated. Anyone can scrape `/metrics` at high frequency, and `/health` can be hammered during deployments.

What cannot be done without it: security (infrastructure detail exposure), DoS prevention, scraping abuse prevention, compliance, incident response stability.

**Code blocks:**

```go
// main.go — rate limit the monitoring endpoints themselves
// /metrics is now rate-limited by the rate limiter itself
// /health has a higher limit (normal LB frequency) but still protected

func main() {
    limiter := createLimiter()

    router := mux.NewRouter()

    // /health: generous limit (1000/min) to allow normal LB checks
    router.Handle("/health", rateLimit(limiter, "health-check", Policy{Limit: 1000, Window: 60e9})(healthHandler))

    // /metrics: standard limit (100/min) — scrapers shouldn't hit this hard
    router.Handle("/metrics", rateLimit(limiter, "metrics-scraper", Policy{Limit: 100, Window: 60e9})(metricsHandler))

    // Basic auth on /metrics (configurable via env vars)
    if metricsUser != "" && metricsPass != "" {
        router.Handle("/metrics", basicAuth(router.Handle("/metrics", ...), metricsUser, metricsPass))
    }
}
```

```go
// middleware/ratelimit.go — rate limit middleware
// Wraps handlers with rate limiting using the same limiter instance
// Returns 429 when limit exceeded, includes rate limit headers

func rateLimit(l Limiter, identity string, policy limiter.Policy, h http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        result, _ := l.Check(identity, policy)
        if !result.Allowed {
            w.Header().Set("Retry-After", fmt.Sprintf("%d", int(result.RetryAfter.Seconds())))
            w.WriteHeader(http.StatusTooManyRequests)
            return
        }
        h(w, r)
    }
}
```

**Technical & engineering decisions:**
- Self-rate-limiting: the rate limiter limits itself — consistent experience for consumers
- Higher limit on /health: normal load balancers check every 5-10s (600-1200/min), so 1000/min accommodates this
- Basic auth on /metrics: additional layer for sensitive infrastructure data
- IP allowlist (optional): for production, restrict /metrics to known scrapers

---

## Fix 12: CORS Headers

No CORS headers on any endpoint. Browser-based applications cannot call the rate limiter API directly.

What cannot be done without it: browser consumption, admin dashboards, frontend JS integration, development experience, third-party integrations.

**Code blocks:**

```go
// middleware/cors.go — CORS middleware
// Handles preflight OPTIONS requests
// Configurable origins, methods, headers via flags or env vars

func corsMiddleware(origin string, allowMethods string, allowHeaders string) func(http.Handler) http.Handler {
    return func(h http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Set CORS headers on every response
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Access-Control-Allow-Methods", allowMethods)
            w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
            w.Header().Set("Access-Control-Allow-Credentials", "true")

            // Handle preflight OPTIONS request
            if r.Method == http.MethodOptions {
                w.WriteHeader(http.StatusNoContent)
                return
            }

            h.ServeHTTP(w, r)
        })
    }
}
```

```go
// main.go — CORS applied to all API routes
// Default: allow all origins (configurable for production)
// Preflight handled automatically by middleware

func main() {
    cors := corsMiddleware(
        os.Getenv("CORS_ORIGIN"),           // default: *
        "GET, POST, OPTIONS",                // only these methods allowed
        "Content-Type, Authorization, X-RateLimit-*", // these headers allowed
    )

    router := cors(mux.NewRouter())
    // ... routes ...
}
```

**Technical & engineering decisions:**
- `*` default origin: easiest for development, restrictive for production (set specific origin)
- Preflight handling in middleware: browser sends OPTIONS before POST/PUT — must handle it
- `Access-Control-Allow-Credentials: true`: needed when auth cookies/tokens are involved
- Environment variable configuration: CORS policy changes without code changes

---

## Fix 13: Root config/config.go Cleanup

A 2-line `config/config.go` at the repo root duplicates `internal/config/config.go`. Creates confusion about which config package to import.

What cannot be done without it: clean architecture, single source of truth, safe refactoring, documentation clarity.

**Code blocks:**

```go
// internal/config/config.go — THE canonical config package (only one)
// ServerConfig: all server configuration fields
// LoadConfig(): reads config from environment variables
// NewLogger(): creates structured logger with log level from config
// This is the single source of truth — no other config package should exist

package config

type ServerConfig struct {
    Port           string
    LogLevel       string
    RateLimitCheckURL string
    TLS            TLSConfig
    Redis          RedisConfig
    // ... all server config ...
}

func LoadConfig() (*ServerConfig, error) {
    // Reads from environment variables with sensible defaults
    // No external dependencies — works everywhere
}

func NewLogger(level string) *slog.Logger {
    // Structured JSON logger, level from config
}
```

```go
// Root config/config.go — DELETED
// Was a 2-line stub. No code should import this anymore.
// All imports should point to internal/config
```

**Technical & engineering decisions:**
- Single package (`internal/config`): no ambiguity, one place to look
- `internal/` import restriction: consumers within the module can import it, external consumers use HTTP API instead
- Environment-based config: no config file management, works in Docker/K8s via env vars
- No duplication: if a field is added, it goes in one place only

---

## Summary Table

| Fix | Problem | Impact Without It |
|-----|---------|-------------------|
| 1 | No Grafana dashboards | No monitoring, no alerting |
| 2 | Custom text collector | No Go runtime metrics |
| 3 | Immediate exit on signal | Dropped requests during deployments |
| 4 | HTTP only | Unencrypted, non-compliant |
| 5 | No request timeouts | Cascading failures, goroutine leaks |
| 6 | No pprof | No production diagnostics |
| 7 | No distributed tracing | No latency attribution |
| 8 | No K8s manifests | No orchestration, no scaling |
| 9 | No binary releases | Must compile from source |
| 10 | Single Redis instance | Single point of failure |
| 11 | /metrics and /health unthrottled | DDoS, info disclosure |
| 12 | No CORS | No browser integration |
| 13 | Duplicate config package | Confusing architecture |

---

## Priority Order

| Priority | Fix | Reason |
|----------|-----|--------|
| P0 | Request timeouts (#5) | Cascading failure risk |
| P0 | Graceful shutdown (#3) | Dropped requests every deployment |
| P0 | Redis Sentinel/Cluster (#10) | Single point of failure for storage |
| P1 | Grafana dashboards (#1) | Cannot monitor what you can't see |
| P1 | Official Prometheus client (#2) | Runtime metrics needed |
| P1 | HTTPS/TLS (#4) | Security and compliance |
| P1 | pprof endpoint (#6) | Production debugging essential |
| P1 | Distributed tracing (#7) | Latency attribution |
| P2 | K8s manifests (#8) | Deployment automation |
| P2 | GitHub releases (#9) | Developer experience |
| P2 | Rate limit /metrics+health (#11) | Security hardening |
| P2 | CORS headers (#12) | Browser consumption |
| P2 | Config cleanup (#13) | Architecture clarity |

---

## Technical & Engineering Decisions (All Fixes)

**General patterns across all fixes:**

- **Environment-based configuration**: Every fix uses env vars for config — no hardcoded values, works in Docker/K8s, no rebuild needed for changes
- **Opt-in features**: pprof, TLS, basic auth — all disabled by default, enabled when needed. Don't expose features that aren't needed by default
- **Same interface, different implementations**: Redis Sentinel, Redis Cluster, and MemoryStorage all implement `Storage`. Algorithms don't know which backend is used
- **Fail-open vs fail-closed**: On timeout/storage error, rate limiter fails open (allows request). On invalid request/policy, fails closed (denies). Degradation is graceful
- **Standard tools**: prometheus/client_golang, net/http/pprof, OpenTelemetry, goreleaser — use the ecosystem standards, don't build custom alternatives
- **Config at module level**: Single `internal/config` package. `pkg/rateclient` for the HTTP client (external consumption). `cmd/rate-limiter` for the service binary
- **15s Prometheus scrape interval**: matches typical rate limiter check frequency
- **30s shutdown timeout**: balances request completion vs forced termination
- **2s storage timeout**: detects slow storage without false positives on normal Redis latency
- **Hibernate-style cooldowns**: circuit breaker extends cooldown to 3x normal duration — bad decisions need longer recovery periods
