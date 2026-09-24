# Atomicity Guarantees

This document records the actual atomicity guarantees for each rate-limiting
algorithm against each storage backend, as of this checkpoint. It is the
source of truth referenced by `improvements.md` (section 1.3 and 1.4).

## Interfaces

- `Storage` (`internal/storage/interface.go`): the generic `Get`/`Set`/`Delete`
  interface. **`Get` + `Set` is a non-atomic read-modify-write sequence.** Any
  backend implementing only `Storage` can race under concurrent requests.
- `AtomicCounterStorage` (`internal/storage/atomic.go`): capability interface
  extending `Storage` with single-operation Lua-backed counters
  (`CheckAndSet`, `CheckAndSetCount`, `CheckAndSetTokenBucket`,
  `CheckAndSetLeakyBucket`). A limiter selects the atomic path via a type
  assertion; otherwise it falls back to `Get`/`Set`.

The in-process algorithms also hold a `sync.Mutex` around the whole `Check`,
so they are atomic **only within a single process**. Distributed correctness
requires a backend that implements `AtomicCounterStorage`.

## Per-algorithm matrix

| Algorithm              | Memory (single process)        | Redis (standalone)                       | Redis Cluster                    | Redis Sentinel                  |
|------------------------|--------------------------------|------------------------------------------|----------------------------------|---------------------------------|
| Fixed Window           | atomic (mutex)                 | atomic (Lua `CheckAndSet`)               | non-atomic (`Get`/`Set`)         | non-atomic (`Get`/`Set`)        |
| Sliding Window Counter | atomic (mutex)                 | atomic (Lua `CheckAndSetCount`)          | non-atomic (`Get`/`Set`)         | non-atomic (`Get`/`Set`)        |
| Token Bucket           | atomic (mutex)                 | atomic (Lua `CheckAndSetTokenBucket`)    | non-atomic (`Get`/`Set`)         | non-atomic (`Get`/`Set`)        |
| Leaky Bucket           | atomic (mutex)                 | atomic (Lua `CheckAndSetLeakyBucket`)    | non-atomic (`Get`/`Set`)         | non-atomic (`Get`/`Set`)        |
| Sliding Window Log     | atomic (mutex)                 | **non-atomic** (no Lua method; only `Get`/`Set`) | non-atomic (`Get`/`Set`) | non-atomic (`Get`/`Set`)        |

Key consequences:

1. `RedisClusterStorage` and `RedisSentinelStorage` (in
   `internal/storage/redis_cluster.go` and `redis_sentinel.go`) implement
   only the generic `Storage` interface. They do **not** expose the atomic
   Lua methods. Consequently every algorithm that depends on
   `AtomicCounterStorage` silently degrades to non-atomic `Get`/`Set` against
   these backends. Do not advertise distributed correctness for Cluster/Sentinel
   until the Lua methods are surfaced.
2. `SlidingWindowLog` has **no** Redis atomic path at all. It always uses
   `Get`/`Set`, even against standalone `RedisStorage`. Under concurrent,
   distributed traffic its log can be corrupted (lost updates). Use it only for
   single-instance or low-contention workloads until a Lua implementation is
   added.
3. Production constructors must therefore verify that the configured storage
   satisfies `AtomicCounterStorage` when distributed atomicity is required, and
   must refuse or warn otherwise rather than silently degrading.

## Redis Lua contract

Each atomic method evaluates check + increment inside a single `EVAL` so that
`redis.call('GET', KEYS[1])` + mutation + `SET` is atomic server-side. This
makes the per-key operation race-free across instances. Key caveats:

- Atomicity is **per key**. Operations that span multiple keys are not atomic
  and require a hash-tag strategy (`{key}`) to pin keys to one cluster slot.
- `RedisStorage.Get` returns `(Record{}, false, nil)` on `redis.Nil` and
  propagates other errors; on exhausting retries it returns an error. Callers
  currently treat a `Get` failure as "not found" (fail-open for availability),
  which can briefly permit bursts during an outage (see `improvements.md` 1.5).
- `SlidingWindowCounter.CheckAndSetCount` reads `time.Now()` on the application
  host, not server time. Clock skew between instances affects the blend ratio.

## Composite limiter consumption semantics

`CompositeLimiter` (`internal/limiter/composite.go`) evaluates dimensions in
order:

- If every child implements `ReservingLimiter`, a single gate serializes the
  run. Each child reserves capacity and returns a rollback function. If any
  child later rejects, all prior reservations are rolled back, so the set is
  atomic across tiers. `checkReserved` is the safe mode.
- Otherwise (`checkSequential`), each child is checked via `Get`/`Set`. A
  rejection after one or more successful prior children leaves those children
  consumed — this is the documented partial-consumption fallback. Callers must
  opt into `ReservingLimiter` to opt out of partial consumption.

## Time and clock assumptions

Algorithms read wall-clock time. Inject `limiter.Clock` (in-memory) for
deterministic tests; `RealClock` is the production default. Redis scripts take
their "now" from the application host (`time.Now().Unix()`), so instance clock
skew affects sliding-window decay and Retry-After. For distributed precision,
prefer Redis server time as the single source of truth (future work).
