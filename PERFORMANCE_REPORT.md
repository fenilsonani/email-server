# Performance Report

Snapshot from January 9, 2026 after memory optimizations. Server: 1 vCPU AMD EPYC, 1.9 GB RAM, Ubuntu 24.04.

## Results

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| RSS Memory | 282 MB | 27.4 MB | -90% |
| Peak Memory | 459 MB | 71 MB | -85% |
| cgroup Memory | 150 MB | 9.4 MB | -94% |
| CPU | 0.7% | 0.1% | — |

Peak usage dropped from 90% to 14% of the 512 MB cgroup limit.

## Optimizations

1. **UserRateLimiter cleanup** — Added periodic eviction (every 15 min, entries older than 48h) to fix unbounded map growth.
2. **Pre-compiled regex** — `bodyTagRegex` and `linkHrefRegex` compiled at init instead of per-request.
3. **IMAP map pre-allocation** — Reduced GC pressure from map growth.
4. **Avoid string copies** — `bytes.NewReader(data)` instead of `strings.NewReader(string(data))`.
5. **Vacation response limits** — Semaphore capping concurrent vacation responses at 10.

## Storage at Time of Report

| Component | Size |
|-----------|------|
| Maildir | 40 MB |
| Database | 860 KB |
| Queue | 876 KB |
| Redis | 1.58 MB |

8 users across 2 domains, 6 concurrent IMAP connections at idle.
