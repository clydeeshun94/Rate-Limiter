# PR Status — Active Development 🚧

| PR | Branch | Status |
|----|--------|--------|
| [#1](https://github.com/clydeeshun94/Rate-Limiter/pull/1) | `fix/concurrency-tests` | ✅ Merged |
| [#2](https://github.com/clydeeshun94/Rate-Limiter/pull/2) | `fix/sliding-window-log-storage` | ✅ Merged |
| [#3](https://github.com/clydeeshun94/Rate-Limiter/pull/3) | `fix/http-service-wrapper` | ✅ Merged |
| [#4](https://github.com/clydeeshun94/Rate-Limiter/pull/4) | `fix/redis-storage` | ✅ Merged |
| [#5](https://github.com/clydeeshun94/Rate-Limiter/pull/5) | `fix/rate-limit-headers` | ✅ Merged |
| [#6](https://github.com/clydeeshun94/Rate-Limiter/pull/6) | `fix/adjuster-hysteresis` | ✅ Merged |
| [#7](https://github.com/clydeeshun94/Rate-Limiter/pull/7) | `fix/default-metric-provider` | ✅ Merged |
| [#8](https://github.com/clydeeshun94/Rate-Limiter/pull/8) | `fix/integration-tests` | ✅ Merged |
| [#9](https://github.com/clydeeshun94/Rate-Limiter/pull/9) | `docs/readme` | ✅ Merged |
| [#10](https://github.com/clydeeshun94/Rate-Limiter/pull/10) | `fix/docker-and-binary` | ✅ Merged |
| [#11](https://github.com/clydeeshun94/Rate-Limiter/pull/11) | `fix/admin-api` | ✅ Merged |
| [#12](https://github.com/clydeeshun94/Rate-Limiter/pull/12) | `fix/logging` | ✅ Merged |
| [#17](https://github.com/clydeeshun94/Rate-Limiter/pull/17) | `fix/build-fix` | ✅ Merged |
| [#18](https://github.com/clydeeshun94/Rate-Limiter/pull/18) | `fix/per-identity-storage` | ✅ Merged |
| [#19](https://github.com/clydeeshun94/Rate-Limiter/pull/19) | `fix/redis-ttl` | ✅ Merged |
| [#20](https://github.com/clydeeshun94/Rate-Limiter/pull/20) | `fix/config-env` | ✅ Merged |
| [#21](https://github.com/clydeeshun94/Rate-Limiter/pull/21) | `fix/redis-service` | ✅ Merged |
| [#22](https://github.com/clydeeshun94/Rate-Limiter/pull/22) | `fix/admin-auth` | ✅ Merged |
| [#23](https://github.com/clydeeshun94/Rate-Limiter/pull/23) | `fix/observability-configs` | ✅ Merged |
| [#24](https://github.com/clydeeshun94/Rate-Limiter/pull/24) | `fix/observability-configs` | ✅ Merged |

## Documentation PRs (Rate Limiter internal)

| PR | Branch | Status | Notes |
|----|--------|--------|-------|
| [#25](https://github.com/clydeeshun94/Rate-Limiter/pull/25) | `docs/comments-fixed_window` | ✅ Merged | Comments: fixed_window.go |
| [#26](https://github.com/clydeeshun94/Rate-Limiter/pull/26) | `docs/comments-sliding_window_counter` | 🟡 Open | Comments: sliding_window_counter.go |
| [#27](https://github.com/clydeeshun94/Rate-Limiter/pull/27) | `docs/comments-leaky_bucket` | ✅ Merged | Comments: leaky_bucket.go |
| [#28](https://github.com/clydeeshun94/Rate-Limiter/pull/28) | `docs/comments-token_bucket` | 🟡 Open | Comments: token_bucket.go |
| [#29](https://github.com/clydeeshun94/Rate-Limiter/pull/29) | `docs/comments-sliding_window_log` | 🟡 Open | Comments: sliding_window_log.go |
| [#30](https://github.com/clydeeshun94/Rate-Limiter/pull/30) | `docs/comments-adjuster` | 🟡 Open | Comments: adjuster.go |
| [#31](https://github.com/clydeeshun94/Rate-Limiter/pull/31) | `docs/comments-collector` | 🟡 Open | Comments: metrics/collector.go |
| [#32](https://github.com/clydeeshun94/Rate-Limiter/pull/32) | `docs/comments-interface` | 🟡 Open | Comments: storage/interface.go |
| [#33](https://github.com/clydeeshun94/Rate-Limiter/pull/33) | `docs/comments-memory` | 🟡 Open | Comments: storage/memory.go |
| [#34](https://github.com/clydeeshun94/Rate-Limiter/pull/34) | `docs/comments-redis` | 🟡 Open | Comments: storage/redis.go |
| [#35](https://github.com/clydeeshun94/Rate-Limiter/pull/35) | `docs/comments-logger` | 🟡 Open | Comments: pkg/logging/logger.go |
| [#36](https://github.com/clydeeshun94/Rate-Limiter/pull/36) | `docs/comments-client` | 🟡 Open | Comments: pkg/rateclient/client.go |
| [#37](https://github.com/clydeeshun94/Rate-Limiter/pull/37) | `docs/comments-limiter_test` | 🟡 Open | Comments: tests/integration/limiter_test.go |
| [#38](https://github.com/clydeeshun94/Rate-Limiter/pull/38) | `docs/comments-makefile` | 🟡 Open | Comments: Makefile |

## External PRs (Pull Shark)

| PR | Branch | Status | Notes |
|----|--------|--------|-------|
| [#81](https://github.com/ulikunitz/xz/pull/81) | `master` | 🟡 Open | Fix: retry (0,nil) reads in breader.ReadByte |
| [#2](https://github.com/clydeeshun94/resty/pull/2) | `fix/cookiejar-error` | 🟡 Open | Fix: handle error from cookiejar.New in createCookieJar (clean 1-commit PR, replaces #1) |

> Both PRs target external repos (required for Pull Shark badge). 2 merged = badge.

### Closed PRs

| PR | Reason |
|----|--------|
| [#816](https://github.com/gorilla/mux/pull/816) (gorilla/mux) | Marked duplicate of #810 by maintainer thaJeztah |

## URL Shortener (clydeeshun94/URL-Shorterner)

| Commit | Description |
|--------|-------------|
| 579f8bd | Add rate limiter integration via HTTP /check endpoint |
| d127c80 | Initial commit (server, db, frontend, tests) |
| 23707ed | Add health check endpoint |
| e85ed09 | Add graceful shutdown on SIGTERM/SIGINT |
| 8a31c71 | Remove dead apiRateLimit function |
| b0f1044 | Add response time to request logging |

## Summary

- **Total PRs:** 38
- **Merged:** 26
- **Open:** 12
- **Fixes from fixes.md (1–13):** All ✅
- **Review fixes (14–21):** All ✅
- **Documentation (25–38):** 14 PRs open (1 merged: #25, #27)
- **Build:** ✅ Passing
- **Tests:** ✅ All passing
- **Vet:** ✅ Clean
