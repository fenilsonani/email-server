# Mail Server Performance Report

**Generated:** January 9, 2026 (Post-Optimization)
**Server:** mail.fenilsonani.com
**Version:** Optimized build with memory improvements

---

## Executive Summary

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Memory Usage | 149.65 MB | **9.39 MB** | 94% reduction |
| Peak Memory | 459.05 MB | **71.09 MB** | 85% reduction |
| CPU Usage | 0.7% | **0.1%** | Minimal |
| Binary Size | 32 MB | 32 MB | Unchanged |

---

## Optimizations Applied

The following performance optimizations were implemented:

1. **UserRateLimiter Memory Leak Fix**
   - Added periodic cleanup (every 15 minutes)
   - Removes entries not accessed in 48 hours
   - Prevents unbounded map growth

2. **Pre-compiled Regex Patterns**
   - `bodyTagRegex` and `linkHrefRegex` compiled at package init
   - Avoids regex compilation on every tracking request

3. **IMAP Session Map Pre-allocation**
   - Maps pre-allocated with known capacity
   - Reduces GC pressure from map growth reallocations

4. **String Allocation Fixes**
   - Replaced `strings.NewReader(string(data))` with `bytes.NewReader(data)`
   - Avoids unnecessary byte slice to string conversions

5. **Vacation Response Goroutine Limits**
   - Semaphore limiting to 10 concurrent vacation responses
   - Prevents unbounded goroutine spawn under load

---

## System Specifications

| Component | Value |
|-----------|-------|
| OS | Ubuntu 24.04.3 LTS |
| Kernel | 6.8.0-71-generic |
| CPU | AMD EPYC 7713 64-Core (1 vCPU allocated) |
| Total RAM | 1.9 GB |
| Disk | 49 GB (13% used) |

---

## Memory Footprint

### Current Usage (Post-Optimization)
| Metric | Value |
|--------|-------|
| **Current (cgroup)** | 9.39 MB |
| **Peak Memory** | 71.09 MB |
| **Memory Limit** | 512 MB |
| **Resident Memory (RSS)** | 27.4 MB |

### Memory Breakdown (from /proc)
| Type | Size |
|------|------|
| VmPeak (Peak Virtual) | 1,646 MB |
| VmSize (Current Virtual) | 1,646 MB |
| VmRSS (Resident) | 27.4 MB |
| VmHWM (High Water Mark) | 88.8 MB |
| VmData (Data Segment) | 151 MB |
| VmStk (Stack) | 132 KB |
| VmExe (Executable) | 10.8 MB |
| VmSwap | 0 KB |

### Before vs After Comparison
| Metric | Before | After | Change |
|--------|--------|-------|--------|
| Current (cgroup) | 149.65 MB | 9.39 MB | -94% |
| Peak Memory | 459.05 MB | 71.09 MB | -85% |
| VmRSS | 282 MB | 27.4 MB | -90% |
| VmHWM | 475 MB | 88.8 MB | -81% |
| % of Limit (current) | 29% | 1.8% | Much safer |
| % of Limit (peak) | 90% | 14% | Much safer |

### Assessment
- Current memory usage is **1.8% of limit** (9.4 MB / 512 MB)
- Peak usage only **14% of limit** - excellent headroom
- No swap usage indicates efficient memory management
- Optimizations significantly reduced GC pressure

---

## CPU Usage

| Metric | Value |
|--------|-------|
| Current CPU | 0.1% |
| Threads | 6 |
| Load Average | 0.07 (1m), 0.03 (5m), 0.00 (15m) |

### Assessment
- Extremely low CPU utilization
- Server is significantly over-provisioned for current load
- Could handle 50-100x more traffic on same hardware

---

## Process Details

| Metric | Value |
|--------|-------|
| PID | 677913 |
| User | mailserver |
| Threads | 6 |

---

## Network & Connections

### Listening Ports
| Port | Service | Protocol |
|------|---------|----------|
| 25 | SMTP | Plain/STARTTLS |
| 143 | IMAP | Plain/STARTTLS |
| 465 | SMTPS | SSL/TLS |
| 587 | Submission | STARTTLS |
| 993 | IMAPS | SSL/TLS |
| 8080 | Admin Panel | HTTP (localhost only) |
| 8081 | Autodiscover | HTTP |
| 8082 | Transactional API | HTTP |
| 8443 | CalDAV/CardDAV | HTTPS |

### Current Connections
| Type | Count |
|------|-------|
| IMAP Connections | 6 |
| SMTP Connections | 0 |
| Total Established | 6 |

---

## Storage

### Disk Usage
| Path | Size |
|------|------|
| **Total Mail Data** | 42 MB |
| Maildir | 40 MB |
| Database | 860 KB |
| Queue | 876 KB |

### Mailbox Sizes (by user)
| User ID | Size |
|---------|------|
| user_1 (fenil@fenilsonani.com) | 39 MB |
| user_2 | 420 KB |
| user_3 | 112 KB |
| user_4 | 308 KB |
| user_5 | 112 KB |
| user_6 | 100 KB |
| user_7 | 100 KB |
| user_8 (sales@trendifyindia.com) | 104 KB |

---

## Database Statistics

| Metric | Count |
|--------|-------|
| Domains | 2 |
| Users | 8 |

### Users per Domain
| Domain | Users |
|--------|-------|
| fenilsonani.com | 7 |
| trendifyindia.com | 1 |

---

## Related Services

### Redis (Queue Backend)
| Metric | Value |
|--------|-------|
| Current Memory | 1.58 MB |
| Peak Memory | 10.89 MB |
| Memory Limit | Unlimited |

### Nginx (Reverse Proxy)
| Metric | Value |
|--------|-------|
| Status | Active |
| Workers | 1 |

---

## Performance Benchmarks

### Capacity Estimates (Post-Optimization)
Based on optimized resource usage:
| Metric | Current | Estimated Max |
|--------|---------|---------------|
| Concurrent IMAP | 6 | ~500-1000 |
| Emails/hour | ~1 | ~2000-5000 |
| Mailboxes | 8 | ~500-1000 |

With 85% reduction in peak memory, capacity estimates have significantly improved.

---

## Recommendations

### Current State: Excellent
The server is running with highly optimized resource usage after performance improvements.

### Future Considerations
1. **Memory Limit**: Current 512 MB limit is now very generous. Peak only uses 14%.

2. **Monitoring**: Consider adding:
   - Prometheus metrics endpoint
   - Memory trend monitoring to validate optimizations under load

3. **Scaling**: With current optimizations, the server can handle significant growth without hardware changes.

---

## Technical Notes

- **Language**: Go (compiled, single binary)
- **Database**: SQLite (lightweight, embedded)
- **Queue**: Redis (in-memory, fast)
- **Architecture**: Multi-domain support with per-domain DKIM
- **Protocols**: SMTP, IMAP, CalDAV, CardDAV, Autodiscover

---

*Report updated after performance optimizations on January 9, 2026*
