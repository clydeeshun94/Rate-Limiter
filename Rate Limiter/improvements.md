# Rate Limiter Improvements Roadmap
This document records the current production gaps in the rate limiter and a staged plan for addressing them. It is intentionally based on the implementation as it exists today, not on aspirational features.

## Current state
The package already has a solid algorithmic foundation:

- Five algorithms: fixed window, sliding-window counter, sliding-window log, token bucket, and leaky bucket.
- A `RateLimiter` interface and swappable storage abstraction.
- In-memory storage for local use and Redis storage with Lua-backed atomic operations for several algorithms.
- Redis TTL support in the primary `RedisStorage` implementation.
- Prometheus-style metrics, rate-limit response headers, an HTTP service, admin authentication, integration tests, and adaptive limit adjustment with hysteresis/circuit-breaker behavior.

Those features make this a useful learning and prototype package. They do not yet make it a universally safe drop-in production control plane. The main missing pieces are identity resolution, policy composition, failure semantics, security hardening, and operational proof under distributed failure.

---

## 1. P0 gaps: correctness and safety before new features
These should be resolved before exposing the service to untrusted or high-value traffic.

### 1.1 Identity is caller-supplied and has no resolver boundary
`POST /check` accepts an arbitrary `identity` string. The service does not currently derive identity from an authenticated principal, API key, trusted client IP, session, or endpoint. As a result:

- An unauthenticated client can submit a new identity on every request and bypass the limiter.
- IP-based limiting is not available as a service-level fallback.
- There is no trust model for `X-Forwarded-For`; blindly consuming it would enable spoofing.
- There is no distinction between a user identity and a network-level abuse signal.
- The storage key is effectively whatever the caller provides, so key namespaces and normalization are caller responsibilities.

**Required direction:** introduce an identity-resolution layer rather than making the HTTP body the source of truth.

```go
type IdentityResolver interface {
    Resolve(r *http.Request) (Identity, error)
}

type Identity struct {
    Primary   string // user ID, API key, or session
    Secondary string // trusted IP/network signal
    Source    string
}
```

The resolver should support authenticated user ID, API key, session, trusted proxy IP, and composite identities. It should define precedence explicitly: authenticated principal first, then API key/session, then IP fallback for anonymous traffic. IP should normally be a secondary abuse-control dimension, not the sole quota for authenticated users.

### 1.2 No policy/rule model for endpoint-specific limits
The current request selects an algorithm and sends a policy, but the server keeps one configured limit per algorithm. There is no first-class `resource`, `action`, route, tenant, plan, or cost dimension. Therefore login, health checks, search, uploads, and payments cannot receive different quotas through a centrally managed policy.

The server also overwrites the request limit with the configured algorithm limit, which is safer than allowing clients to raise limits but is not a complete policy system.

**Required direction:** add a validated policy model such as:

```go
type Rule struct {
    Name       string
    Resource   string
    Identity   IdentityClass
    Algorithm  string
    Limit      int
    Window     time.Duration
    Cost       int
}
```

Resolve rules server-side using a deterministic key such as `namespace:resource:identity`. Do not let clients choose arbitrary algorithms, limits, or storage keys in production mode.

### 1.3 No composite/multi-tier limiter
Only one limiter is selected for each `/check` call. The package does not yet provide an atomic or clearly defined `CompositeLimiter` for enforcing burst, minute, hour, day, and tenant-wide constraints together.

A naive loop that checks multiple limiters can partially consume earlier buckets before a later bucket rejects the request. That creates inconsistent accounting. The implementation must document whether partial consumption is acceptable, support reservation/rollback where required, or use a storage script that evaluates all dimensions atomically.

**Required direction:** implement multi-tier policy evaluation with explicit semantics for partial consumption, plus tests for boundary bursts and concurrent requests.

### 1.4 Storage atomicity is not uniform across all backends
The primary Redis storage has Lua operations for fixed window, sliding counter, token bucket, and leaky bucket. However:

- The generic `Storage` interface is still a `Get`/`Set` interface, so non-atomic implementations can fall back to read-modify-write behavior.
- `RedisClusterStorage` and `RedisSentinelStorage` currently implement basic JSON `Get`/`Set`/`Delete`; they do not expose the atomic Lua methods from `AtomicCounterStorage`.
- Cluster scripts need key-slot testing and hash-tag strategy if a future operation spans multiple keys.
- The Redis clients use a background context, so request cancellation and deadlines do not propagate to storage operations.

**Required direction:** define capability interfaces clearly, make production constructors use atomic implementations, and add Redis integration tests for every backend and algorithm under contention. Avoid claiming distributed correctness when the selected backend is only basic `Storage`.

### 1.5 Redis failure behavior is not configurable per risk class
Storage errors are propagated by the limiter, and `/check` currently returns `500`. There is no configured fail-open, fail-closed, or local-fallback mode. Also, retry behavior is fixed and includes sleeps that can increase request latency during an outage.

**Required direction:** add a policy-level failure mode:

```go
type FailMode int
const (
    FailOpen FailMode = iota
    FailClosed
    FailLocal
)
```

Select it by risk/resource, not globally. For example, health/read-only traffic may fail open, while login, password reset, payment, and administrative actions should fail closed or use a conservative local fallback. Add a circuit breaker, bounded exponential backoff with jitter, health state, and metrics for degraded decisions.

### 1.6 Time and clock assumptions are implicit
Algorithms use local wall-clock time and Redis scripts receive timestamps from application instances. Clock skew, NTP corrections, second-level truncation, and inconsistent time sources can affect reset times and sliding-window behavior.

**Required direction:** inject a clock for deterministic tests, define precision requirements, reject or clamp unreasonable timestamps, and decide whether Redis server time should be used for distributed algorithms. Add tests for clock jumps and skew.

---

## 2. P1 gaps: abuse resistance, privacy, and user experience
### 2.1 Shared IPs and IPv6 are not addressed
There is no built-in IP resolver, trusted proxy configuration, IPv4 normalization, or IPv6 prefix policy. A full IP quota would unfairly combine users behind NAT/CGNAT while privacy IPv6 addresses can fragment one user across many addresses.

**Plan:**

- Add a trusted-proxy configuration; never trust forwarding headers from arbitrary clients.
- Normalize IPv4 and IPv6 using the standard library.
- Support coarse network identities: IPv4 `/24` and IPv6 `/64` as configurable defaults, not universal rules.
- Apply network limits as a secondary abuse dimension.
- Add privacy documentation, retention limits, and tests for proxy chains and spoofed headers.

### 2.2 No progressive enforcement or escalation model
All denials are effectively the same. There is no warning state, temporary block, CAPTCHA/step-up signal, reputation score, or sustained-abuse policy. A strict limiter can therefore create false positives without a controlled recovery path.

**Plan:** introduce an `EscalationPolicy` and separate quota decisions from enforcement actions:

1. Normal allowance.
2. Warning/telemetry after the first threshold.
3. Temporary block after repeated violations.
4. Step-up authentication or CAPTCHA for suspicious flows.
5. Long-lived block only through an explicit abuse-management system.

The escalation state must have TTLs, bounded memory, audit events, and an operator override.

### 2.3 No endpoint cost model
Every request currently consumes one unit. Expensive operations cannot consume more budget than cheap operations, and the limiter cannot distinguish request classes.

**Plan:** add request cost to policies and make algorithms consume `cost` tokens/count units atomically. Add route/action mapping and examples for login, password reset, search, upload, and health endpoints. Verify that `Remaining` and `Retry-After` remain meaningful for costs greater than one.

### 2.4 Retry behavior can create synchronized replays
`Retry-After` is currently deterministic. Clients that receive the same value can retry in a synchronized wave when a window opens. The package does not provide a client backoff contract or server-side jitter option.

**Plan:** support bounded server jitter, document exponential backoff with client jitter, and ensure reset calculations never produce negative or excessively large retry values. Test that jitter does not violate the configured maximum wait or make responses misleading.

### 2.5 No idempotency/deduplication support
Mobile and unreliable clients can retry a successful mutation. The limiter sees each retry as a new request. There is no `Idempotency-Key` handling or result replay for safe mutation endpoints.

**Plan:** keep idempotency separate from rate limiting but provide an integration hook. Store a scoped key, request fingerprint, response status/body metadata, and TTL atomically. Reject reuse with a different payload. Do not treat an idempotency key as an identity or as an authorization mechanism.

### 2.6 Identity privacy is incomplete
Metrics hash identities for some purposes, but there is no documented HMAC key configuration/rotation strategy for storage keys and no complete guarantee that raw identities will not enter logs or labels. Plain SHA-256 is vulnerable to guessing low-entropy IDs.

**Plan:**

- HMAC identities before persistence and telemetry using a secret held outside the application image.
- Version key prefixes to support rotation (`v1`, `v2`) and controlled migration.
- Never use raw identity values as Prometheus labels.
- Apply TTLs to all identity and escalation records.
- Redact identities from logs and define retention/deletion behavior.

---

## 3. P1/P2 operational and API gaps
### 3.1 Configuration is not a durable policy control plane
The service supports environment configuration and runtime admin updates, but runtime updates are in-memory and there is no versioned policy store, optimistic concurrency, audit trail, or rollback. A restart can lose changes.

**Plan:** create a versioned policy document with validation, dry-run, rollout, rollback, and audit metadata. Separate read-only status endpoints from mutating admin endpoints. Prefer an external control plane or signed configuration for multi-instance deployments.

### 3.2 Admin and operational endpoints need stronger isolation
Admin authentication is token-based, but there is no role model, token rotation workflow, mTLS option, audit log, or dedicated management listener. The `/crash` endpoint is especially dangerous because it intentionally generates traffic against a target.

**Plan:** put management APIs on a private listener or network, add scoped roles and short-lived credentials, audit every mutation, rate limit the management plane, and gate destructive load-generation functionality behind a separate build/configuration flag. Protect metrics and analytics from information disclosure.

### 3.3 Observability needs bounded, standard metrics
The project has metrics and dashboards, but identity-related cardinality and custom exposition need explicit production safeguards. Operators also need storage latency, error rate, fail-mode decisions, policy name, backend, and limiter decision reason.

**Plan:** use the official Prometheus client, keep labels low-cardinality, add histograms for check/storage latency, and define alerts for denial spikes, Redis errors, fallback activation, policy drift, and clock anomalies. Add tracing with redacted attributes and correlation IDs.

### 3.4 Health checks do not prove dependency readiness
`/health` reports service health and uses a local in-memory limiter. It does not distinguish liveness from readiness or expose Redis/control-plane dependency state.

**Plan:** provide separate liveness and readiness endpoints. Readiness should verify required dependencies and configuration, with a documented behavior during degraded fail-open/local modes. Do not rate limit probes in a way that can make an orchestrator remove a healthy instance.

### 3.5 No explicit resource bounds for high-cardinality state
Sliding-window logs store one timestamp per request and memory storage has no eviction mechanism. High-cardinality identities, long windows, and attacker-controlled identity values can exhaust memory or Redis.

**Plan:** enforce maximum identity length, policy/window bounds, timestamp-entry caps, and per-tenant quotas. Add bounded LRU/TTL behavior for memory storage and monitor Redis memory. Define what happens when a state budget is exceeded.

### 3.6 Test coverage does not yet demonstrate production failure behavior
There are algorithm, concurrency, and some integration tests, but the roadmap needs explicit distributed and chaos coverage:

- Redis outage, timeout, partial failure, and recovery.
- Cluster slot movement and Sentinel failover.
- Multiple instances issuing the same identity concurrently.
- Clock skew and boundary timing.
- Policy reload races and rollback.
- Fuzzing malformed HTTP input and oversized bodies.
- Load tests measuring p50/p95/p99 latency and memory growth.
- Race detector and static analysis in CI.

---

## 4. Recommended implementation order
### Phase 0 — Establish production contracts
1. Add a clock abstraction and deterministic time tests.
2. Define identity, policy, decision, error, and storage capability interfaces.
3. Add request size limits, identity normalization, maximum key lengths, and strict input validation.
4. Document atomicity guarantees for every algorithm/backend combination.
5. Add CI gates: `go test`, `go test -race`, fuzz targets, static analysis, and Redis integration tests.

### Phase 1 — Correct identity and policy enforcement
1. Implement trusted `IdentityResolver` variants: authenticated user, API key, session, IP, and composite.
2. Add endpoint/action/resource to the policy key.
3. Implement server-side rule resolution and policy versioning.
4. Add multi-tier/composite enforcement with clearly tested consumption semantics.
5. Add cost-based requests.

### Phase 2 — Distributed resilience
1. Finish atomic capability support for standalone Redis, Cluster, and Sentinel.
2. Use request-scoped contexts and bounded retries.
3. Implement `FailOpen`, `FailClosed`, and `FailLocal` per policy/resource.
4. Add circuit breaker, local conservative fallback, and degraded-mode metrics.
5. Run failover and contention tests against real Redis topologies.

### Phase 3 — Abuse prevention and privacy
1. Add IPv4/IPv6 network resolvers and trusted proxy handling.
2. Add HMAC keying, secret rotation, redaction, and TTL guarantees.
3. Add progressive escalation and temporary blocks.
4. Add retry jitter and client backoff guidance.
5. Add idempotency integration for mutation endpoints.

### Phase 4 — Operations and ecosystem readiness
1. Harden admin APIs with private networking, roles, rotation, and audit logs.
2. Replace custom metrics exposition with the official Prometheus client.
3. Separate liveness/readiness and document deployment behavior.
4. Add policy rollout, dry-run, rollback, and configuration auditability.
5. Publish an HTTP/OpenAPI contract and language-neutral examples.
6. Benchmark realistic workloads and publish capacity guidance.

---

## Definition of done for an industry-ready release
A release should not be called production-ready until it can demonstrate all of the following:

- Authenticated users and API clients are not collapsed into one shared IP bucket.
- Anonymous traffic is protected by trusted, normalized network identity signals.
- Endpoint-specific, cost-aware, multi-tier policies are enforced consistently.
- Distributed decisions are atomic for the advertised backend/topology.
- Redis outages follow an explicit, tested risk-based fail mode.
- All state has bounded memory/TTL behavior and privacy-safe keying.
- Operators can observe denials, latency, backend health, fallback use, and policy versions without high-cardinality leaks.
- Admin operations are authenticated, authorized, audited, and isolated.
- Concurrency, failover, clock, load, fuzz, and recovery tests pass in CI.
- Clients receive standards-compatible headers and documented retry/idempotency guidance.

The most important next step is **Phase 0 followed by identity and policy enforcement**. Adding more algorithms before those contracts are in place would increase feature count without solving the main production risks.
