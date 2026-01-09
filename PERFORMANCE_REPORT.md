# Mail Server Performance Report

**Generated:** January 9, 2026
**Server:** mail.fenilsonani.com
**Uptime:** 2 weeks, 6 days, 6 hours

---

## Executive Summary

| Metric | Value | Assessment |
|--------|-------|------------|
| Memory Usage | **149.65 MB** (current) | Excellent |
| Peak Memory | 459.05 MB | Within limits |
| CPU Usage | 0.7% | Minimal |
| Binary Size | 32 MB | Compact |
| Response Time | Instant | Excellent |

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

### Current Usage
| Metric | Value |
|--------|-------|
| **Resident Memory (RSS)** | 282 MB |
| **Current (cgroup)** | 149.65 MB |
| **Peak Memory** | 459.05 MB |
| **Memory Limit** | 512 MB |
| **Virtual Memory** | 2.1 GB |
| **Swap Used** | 0 KB |

### Memory Breakdown (from /proc)
| Type | Size |
|------|------|
| VmPeak (Peak Virtual) | 2,153 MB |
| VmSize (Current Virtual) | 2,153 MB |
| VmRSS (Resident) | 282 MB |
| VmHWM (High Water Mark) | 475 MB |
| VmData (Data Segment) | 544 MB |
| VmStk (Stack) | 132 KB |
| VmExe (Executable) | 10.8 MB |
| VmSwap | 0 KB |

### Assessment
- Current memory usage is **29% of limit** (149 MB / 512 MB)
- Peak usage reached **90% of limit** during high load
- No swap usage indicates sufficient RAM allocation
- Memory is efficiently managed with Go's garbage collector

---

## CPU Usage

| Metric | Value |
|--------|-------|
| Current CPU | 0.7% |
| CPU Time Used | 4.01 seconds |
| Threads | 7 |
| Load Average | 0.20 (1m), 0.05 (5m), 0.02 (15m) |

### Assessment
- Extremely low CPU utilization
- Server is significantly over-provisioned for current load
- Could handle 10-50x more traffic on same hardware

---

## Process Details

| Metric | Value |
|--------|-------|
| PID | 675732 |
| User | mailserver |
| State | Ssl (Sleeping, session leader, multi-threaded) |
| Threads | 7 |
| Open File Descriptors | 34 |
| Restart Count | 0 |

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
| IMAP Connections | 12 |
| SMTP Connections | 0 |
| Total Established | 12 |

---

## Storage

### Disk Usage
| Path | Size |
|------|------|
| **Total Mail Data** | 42 MB |
| Maildir | 40 MB |
| Database | 852 KB |
| Queue | 876 KB |
| Backups | 172 KB |
| ACME Certs | 8 KB |

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
| Active Domains | 2 |
| Users | 8 |
| Active Users | 8 |
| Admin Sessions | 4 |
| API Keys | 1 |

### Users per Domain
| Domain | Users |
|--------|-------|
| fenilsonani.com | 7 |
| trendifyindia.com | 1 |

---

## Activity (Last 24 Hours)

| Activity | Count |
|----------|-------|
| Local Email Deliveries | 26 |
| Rejected Emails | 7 |
| Successful IMAP Logins | 441 |
| INFO Log Entries | 1,659 |
| ERROR Log Entries | 23 |

---

## Related Services

### Redis (Queue Backend)
| Metric | Value |
|--------|-------|
| Current Memory | 1.60 MB |
| Peak Memory | 10.89 MB |
| Memory Limit | Unlimited |

### Nginx (Reverse Proxy)
| Metric | Value |
|--------|-------|
| Status | Active |
| Workers | 1 |

---

## Performance Benchmarks

### Per-Email Resource Cost (Estimated)
Based on 26 deliveries with current resource usage:
- **Memory per delivery**: ~5.7 MB peak
- **CPU time per delivery**: ~0.15 seconds

### Capacity Estimates
Based on current usage patterns:
| Metric | Current | Estimated Max |
|--------|---------|---------------|
| Concurrent IMAP | 12 | ~100-200 |
| Emails/hour | ~1 | ~500-1000 |
| Mailboxes | 8 | ~100-500 |

---

## Recommendations

### Current State: Healthy
The server is running efficiently with minimal resource usage.

### Optimizations (Optional)
1. **Memory Limit**: Current 512 MB limit is appropriate. Peak reached 459 MB, consider increasing to 768 MB if more users are added.

2. **Monitoring**: Consider adding:
   - Prometheus metrics endpoint
   - Alerting for memory > 80%
   - Email queue monitoring

3. **Scaling**: For significant growth:
   - Increase vCPU allocation
   - Add Redis persistence
   - Consider read replicas for IMAP

---

## Technical Notes

- **Language**: Go (compiled, single binary)
- **Database**: SQLite (lightweight, embedded)
- **Queue**: Redis (in-memory, fast)
- **Architecture**: Multi-domain support with per-domain DKIM
- **Protocols**: SMTP, IMAP, CalDAV, CardDAV, Autodiscover

---

*Report generated automatically from live server metrics*
