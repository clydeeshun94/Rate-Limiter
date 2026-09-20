# Fixes 8–10: Detailed Explanations

---

## Fix 8: Integration and Stress Tests

### What was wrong before

The test suite had two gaps:

1. **No integration tests.** Every test verified a single algorithm or single component in isolation. But algorithms interact with storage, adjusters interact with algorithms, and the HTTP service interacts with everything. These interactions are where bugs hide — storage not persisting, adjuster not reading limiter state, multiple limiters conflicting.

2. **No stress/benchmark tests.** We had no idea how fast each algorithm was. No throughput measurements. No way to detect performance regressions. A slower algorithm in production could cause request timeouts with no warning.

### What was wrong at the integration level specifically

Consider this scenario: a dev creates `FixedWindow` with `MemoryStorage`, makes some checks, then creates another `FixedWindow` with the same storage and same identity. Does the second instance see the first instance's data? We didn't know — there was no test. What about the Adjuster managing three different limiters simultaneously? No test. The storage persistence contract was unverified.

### What changed

**Integration tests** in `tests/integration/`:
- `limiter_test.go` (10 tests):
  - Per-algorithm storage persistence: Create limiter A, make N checks, create limiter B with same storage, verify B sees A's data
  - All 5 algorithms end-to-end: Each algorithm tested with storage to verify limit enforcement
- `adjuster_test.go` (4 tests):
  - Adjuster + Algorithm + Storage: Verify adjuster works with real storage-backed limiters
  - Multiple algorithms sharing one Adjuster: Verify adjuster can manage 2+ limiters
  - Adjuster adjusts multiple limiters: Verify each limiter is adjusted correctly
  - Adjuster with Sliding Window Log: Verify Swl + Adjuster + Storage interaction

**Benchmark tests** in `internal/limiter/benchmark_test.go` (7 benchmarks):
- Sequential throughput for all 5 algorithms (`Benchmark*_Check`)
- Concurrent throughput for Fixed Window and Sliding Window Log (`Benchmark*_Concurrent`)
- Memory per identity (`BenchmarkFixedWindow_MemoryPerIdentity`)

### Key architectural decisions

**Why integration tests in `tests/integration/` instead of in each package?** Because integration tests cross package boundaries. They test the contract between `internal/limiter`, `internal/storage`, and `internal/adjuster`. Putting them in one place makes them easier to run and maintain. They're in the same module so they can import `internal/` packages freely.

**Why separate `tests/` directory?** Go convention puts tests in the package being tested. But integration tests are NOT unit tests — they test across packages. A separate directory makes the distinction clear: unit tests run fast and in isolation; integration tests are heavier and test real interactions.

**Why benchmark in `internal/limiter/` instead of a separate package?** Benchmarks measure the performance of the limiter package specifically. They need access to internal implementation details. Placing them in-package keeps them close to what they measure.

**Why 1000 goroutines in concurrency tests but 5 in integration persistence tests?** Concurrency tests stress thread safety (many simultaneous operations). Integration persistence tests verify data consistency (sequential operations across instances). Different goals, different loads.

**Why measure memory per identity?** In production, each identity (user, API key, IP) consumes memory in storage. Tracking memory per identity helps predict capacity: "we can handle 10,000 identities at X MB each."

**Why concurrent benchmarks only for Fixed Window and Sliding Window Log?** These are the most concurrency-sensitive algorithms (storage-backed, multi-goroutine access). Token Bucket and Leaky Bucket have internal state that's less contention-prone. But they're still benchmarked sequentially.

### After vs Before summary
| Before | After |
|--------|-------|
| Only unit tests (single component) | Unit + integration + benchmark tests |
| No storage persistence verification | All 5 algorithms verified with storage |
| No adjuster integration | Adjuster + limiter + storage tested |
| No performance baselines | 7 benchmarks for throughput and memory |
| Bugs in integration hidden | Integration bugs found and fixed |

---

## Fix 9: README Documentation

### What was wrong before

Beyond code comments, there was no user-facing documentation. The existing README was 20 lines: a title, 5 algorithm names, and a bullet list of stack choices. A developer looking at this code had no way to know:

- How to install it
- How to use it (quick start)
- Which algorithm to choose and when
- How to configure the HTTP service
- How to configure Redis
- How the HTTP API works
- How metrics/Prometheus setup works
- How to contribute

A library without docs is invisible. GitHub README is the front door.

### What changed

Comprehensive README covering all 8 sections from fixes.md:

1. **What it is** — One paragraph overview of the project as a production-grade, modular rate limiting engine
2. **Quick Start** — 6 lines of working Go code (import, create storage, create limiter, check, print)
3. **Algorithm Comparison** — Table with 5 algorithms, use cases, trade-offs, and storage usage. Plus guidance on which to choose for each scenario
4. **Installation** — 4 methods: `go get`, build binary, Dockerfile, docker run
5. **Configuration** — Algorithm selection (HTTP), Redis config, Adjuster config with all fields
6. **HTTP API** — `/check`, `/limit`, `/metrics`, `/health` with request/response examples and header docs
7. **Metrics/Prometheus** — Example output + `Wrap()` usage
8. **Adaptive Limiting** — How Adjuster works, hysteresis, circuit breaker, with config example
9. **HTTP Client** — `rateclient` usage for non-Go languages
10. **Project Structure** — Directory map showing `internal/`, `cmd/`, `pkg/`, `tests/`
11. **Contributing** — PR workflow referencing fixes.md

### Key architectural decisions

**Why include algorithm comparison table?** A developer evaluating rate limiters needs to pick one. The table answers "which should I use?" in one glance. Without it, they'd have to read 5 algorithm implementations to understand the trade-offs.

**Why both "Installation" and "Quick Start"?** Installation = how to get the code (go get, docker). Quick Start = how to run it (5 lines of code). Different questions, different audiences. Installation helps ops; Quick Start helps developers evaluating the library.

**Why Dockerfile in README?** Docker is the standard deployment method. Even if the dev doesn't use Docker, having it documented shows the project is production-ready. It also serves as the canonical deployment example.

**Why document HTTP API even though library mode is primary?** Because Fix 3 added HTTP service — it's a real deployment option. The API docs make it usable without reading the source code.

**Why include project structure?** The `internal/`, `cmd/`, `pkg/` layout is non-obvious. Documentation explaining the architecture helps developers navigate the codebase and understand the separation of concerns.

**Why "Contributing" section referencing fixes.md?** fixes.md is the roadmap. Contributors need to know what's being worked on and how to contribute. This turns the README from a static document into a portal for collaboration.

### After vs Before summary
| Before | After |
|--------|-------|
| 20-line README with algorithm names | Comprehensive 11-section README |
| No installation docs | 4 installation methods (go get, binary, Docker) |
| No quick start | 6-line working code sample |
| No algorithm guidance | Comparison table + decision guidance |
| No API docs | Full HTTP API with examples |
| No contribution guide | PR workflow linked to fixes.md |
| Invisible library | Visible, usable, contributable project |

---

## Fix 10: Adjuster Strategy Improvement

### What was wrong before

The Adjuster used hardcoded adjustment rates: 10% increase when healthy, 10% or 50% decrease when sick. These rates were determined by a string `Mode` field ("conservative" or "aggressive"). Three problems:

1. **No gradual increase.** When healthy, the limiter jumped to 110% in one step. If the system was healthy but not at full capacity, this was unnecessarily aggressive.

2. **Fixed decrease rate.** The difference between conservative and aggressive was only in the decrease rate (10% vs 50%). There was no middle ground, no fine-grained control.

3. **String-based mode.** `Mode string` is error-prone — typos like `"conservative"` vs `"conservative "` don't cause compile errors. No type safety, no IDE autocomplete, no valid value enforcement.

4. **No rate limiting on adjustments.** Even with hysteresis and circuit breaker, a single adjustment could jump 50%. Under extreme load, 50% is a big change. No way to cap it.

### What changed

**Named strategies** (typed enum instead of string):
- `Conservative`: Small, predictable changes. Best for stable, predictable traffic.
- `Aggressive`: Large changes. Best for rapidly changing environments where quick adaptation matters more than stability.
- `Adaptive`: Medium changes based on severity. Best for general-purpose use.

**Configurable parameters:**
- `MaxChange` (float64): Maximum percentage change per adjustment cycle. Acts as a hard cap regardless of strategy. Default matches strategy rate.
- `Smoothing` (float64): Multiplier applied to base rate. Default 1.0 (full rate). Set to 0.5 for half-rate adjustments. Useful for extremely sensitive systems.

**Updated `calculateNewLimit`:**
- Uses `getIncreaseRate()` / `getDecreaseRate()` based on Strategy
- Applies Smoothing factor
- Caps adjustment to MaxChange percentage
- Enforces Min/Max bounds

### Key architectural decisions

**Why named strategies instead of string mode?** Type safety. `AdjustmentStrategy` is an `int` enum (`iota`). Typos are compile errors. IDE shows valid values. The compiler enforces correctness. This is idiomatic Go — enums over strings for finite state.

**Why keep `Mode string` field?** Backward compatibility. Existing code using `Mode: "aggressive"` still works — `NewAdjuster` converts it to `Strategy: Aggressive`. Old configs don't break.

**Why defaults match strategy rates?** So users don't experience unexpected capping. Conservative at 10% means Conservative's default MaxChange is 10% — the strategy rate flows through unmodified. If a user wants to cap it, they explicitly set `MaxChange: 0.05`.

**Why Smoothing?** Some systems are extremely sensitive. A 10% adjustment is still significant. Smoothing at 0.5 makes it 5%. This gives ops teams a dial for fine-tuning without changing the strategy. It's a separate knob from strategy selection.

**Why MaxChange defaults match strategy rates?** Because the strategy defines the desired behavior. MaxChange is a safety cap, not a primary control. Default should not interfere with the primary control.

**Why `getIncreaseRate()` and `getDecreaseRate()` as separate methods?** Because increase and decrease rates can diverge independently. Conservative: 10% up, 10% down (symmetric). Aggressive: 20% up, 50% down (asymmetric — cut fast). Adaptive: 15% up, 30% down (somewhat asymmetric). Separation makes the relationship clear and allows independent tuning in the future.

**Why `adjustment = float64(limit) * baseRate * Smoothing`?** Percentage-based calculation. `limit * rate` gives the actual number of requests to add/remove. This is intuitive: "add 10% of current limit" = `limit * 0.10`. Float arithmetic allows precise calculations, then `int()` truncates for the final limit value.

### Strategy Comparison Table

| Strategy | Increase | Decrease | Default MaxChange | Best For |
|----------|----------|----------|-------------------|----------|
| Conservative | 10% | 10% | 10% | Stable, predictable traffic |
| Aggressive | 20% | 50% | 50% | Rapidly changing environments |
| Adaptive | 15% | 30% | 30% | General-purpose use |

### After vs Before summary
| Before | After |
|--------|-------|
| Hardcoded 10%/50% rates | Named strategies with configurable rates |
| String Mode field | Typed enum (AdjustmentStrategy) |
| No adjustment cap | MaxChange percentage cap |
| No fine-tuning | Smoothing factor |
| 3 behaviors (conservative/aggressive/custom) | 3 strategies + configurable parameters |
| No type safety | Compile-time enforcement |
| Backward incompatible potential | Backward compatible (Mode auto-converts) |

10/13 fixes complete. Ready for Fix 11.
