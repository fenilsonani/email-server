package smtp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// BenchmarkGenerateID benchmarks unique ID generation.
func BenchmarkGenerateID_SMTP(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		generateID()
	}
}

// BenchmarkGenerateID_Parallel benchmarks parallel ID generation.
func BenchmarkGenerateID_SMTP_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			generateID()
		}
	})
}

// BenchmarkParseAddress benchmarks address parsing.
func BenchmarkParseAddress_SMTP(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseAddress("user@example.com")
	}
}

// BenchmarkParseAddress_WithBrackets benchmarks address parsing with brackets.
func BenchmarkParseAddress_WithBrackets(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseAddress("<user@example.com>")
	}
}

// BenchmarkParseAddress_Complex benchmarks complex address parsing.
func BenchmarkParseAddress_Complex(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseAddress("<user+tag@subdomain.example.com>")
	}
}

// BenchmarkRateLimiter_CheckAndIncrement benchmarks rate limiter check.
func BenchmarkRateLimiter_CheckAndIncrement(b *testing.B) {
	rl := NewUserRateLimiter(1000000, 10000000) // High limits to avoid hitting them
	defer rl.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.CheckAndIncrement(int64(i % 100)) // Spread across 100 users
	}
}

// BenchmarkRateLimiter_CheckAndIncrement_SingleUser benchmarks single user rate limiting.
func BenchmarkRateLimiter_CheckAndIncrement_SingleUser(b *testing.B) {
	rl := NewUserRateLimiter(1000000, 10000000)
	defer rl.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.CheckAndIncrement(1)
	}
}

// BenchmarkRateLimiter_CheckAndIncrement_Parallel benchmarks parallel rate limiting.
func BenchmarkRateLimiter_CheckAndIncrement_Parallel(b *testing.B) {
	rl := NewUserRateLimiter(1000000, 10000000)
	defer rl.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		userID := int64(0)
		for pb.Next() {
			rl.CheckAndIncrement(userID % 100)
			userID++
		}
	})
}

// BenchmarkRateLimiter_NewUser benchmarks new user counter creation.
func BenchmarkRateLimiter_NewUser(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rl := NewUserRateLimiter(100, 1000)
		rl.CheckAndIncrement(1)
		rl.Stop()
	}
}

// BenchmarkSession_Reset benchmarks session reset.
func BenchmarkSession_Reset(b *testing.B) {
	session := &Session{
		from:  "sender@example.com",
		rcpts: []string{"r1@example.com", "r2@example.com", "r3@example.com"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Reset()
		// Restore for next iteration
		session.from = "sender@example.com"
		session.rcpts = []string{"r1@example.com", "r2@example.com", "r3@example.com"}
	}
}

// BenchmarkSession_Logout benchmarks session logout.
func BenchmarkSession_Logout(b *testing.B) {
	session := &Session{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Logout()
	}
}

// BenchmarkSession_AuthMechanisms benchmarks auth mechanisms.
func BenchmarkSession_AuthMechanisms(b *testing.B) {
	session := &Session{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = session.AuthMechanisms()
	}
}

// BenchmarkRateLimiter_Cleanup benchmarks cleanup operation.
func BenchmarkRateLimiter_Cleanup(b *testing.B) {
	rl := NewUserRateLimiter(100, 1000)
	defer rl.Stop()
	// Add some users
	for i := 0; i < 100; i++ {
		rl.CheckAndIncrement(int64(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.cleanup()
	}
}

// BenchmarkContext_Background benchmarks context creation.
func BenchmarkContext_Background(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = context.Background()
	}
}

// BenchmarkContext_WithTimeout benchmarks context with timeout.
func BenchmarkContext_WithTimeout(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cancel()
		_ = ctx
	}
}

// BenchmarkSession_ContextErr benchmarks context error check.
func BenchmarkSession_ContextErr(b *testing.B) {
	ctx := context.Background()
	session := &Session{ctx: ctx}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = session.ctx.Err()
	}
}

// BenchmarkSession_ContextErr_Cancelled benchmarks cancelled context check.
func BenchmarkSession_ContextErr_Cancelled(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := &Session{ctx: ctx}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = session.ctx.Err()
	}
}

// BenchmarkVacationSemaphore_Acquire benchmarks semaphore acquire.
func BenchmarkVacationSemaphore_Acquire(b *testing.B) {
	sem := make(chan struct{}, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		select {
		case sem <- struct{}{}:
			<-sem
		default:
		}
	}
}

// BenchmarkVacationSemaphore_Parallel benchmarks parallel semaphore usage.
func BenchmarkVacationSemaphore_Parallel(b *testing.B) {
	sem := make(chan struct{}, 10)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			select {
			case sem <- struct{}{}:
				<-sem
			default:
			}
		}
	})
}

// BenchmarkRWMutex benchmarks RWMutex read lock.
func BenchmarkRWMutex_RLock(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mu.RLock()
		mu.RUnlock()
	}
}

// BenchmarkRWMutex_RLock_Parallel benchmarks parallel RWMutex read lock.
func BenchmarkRWMutex_RLock_Parallel(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.RLock()
			mu.RUnlock()
		}
	})
}

// BenchmarkRWMutex_Lock benchmarks RWMutex write lock.
func BenchmarkRWMutex_Lock(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mu.Lock()
		mu.Unlock()
	}
}

// BenchmarkMapAccess benchmarks map access patterns.
func BenchmarkMapAccess_Read(b *testing.B) {
	m := make(map[int64]*userSendCounter)
	for i := int64(0); i < 100; i++ {
		m[i] = &userSendCounter{}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m[int64(i%100)]
	}
}

// BenchmarkMapAccess_Write benchmarks map write patterns.
func BenchmarkMapAccess_Write(b *testing.B) {
	m := make(map[int64]*userSendCounter)
	counter := &userSendCounter{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m[int64(i%100)] = counter
	}
}

// BenchmarkTimeNow benchmarks time.Now() calls.
func BenchmarkTimeNow_SMTP(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		time.Now()
	}
}

// BenchmarkTimeAfter benchmarks time comparison.
func BenchmarkTimeAfter(b *testing.B) {
	now := time.Now()
	later := now.Add(time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = now.After(later)
	}
}

// BenchmarkSliceAppend benchmarks slice append.
func BenchmarkSliceAppend(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var s []string
		s = append(s, "recipient@example.com")
	}
}

// BenchmarkSliceAppend_Preallocated benchmarks preallocated slice.
func BenchmarkSliceAppend_Preallocated(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := make([]string, 0, 10)
		s = append(s, "recipient@example.com")
		_ = s
	}
}

// BenchmarkRecipientSlice_10 benchmarks building recipient slice.
func BenchmarkRecipientSlice_10(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rcpts := make([]string, 0, 10)
		for j := 0; j < 10; j++ {
			rcpts = append(rcpts, "recipient@example.com")
		}
		_ = rcpts
	}
}

// BenchmarkRecipientSlice_100 benchmarks building large recipient slice.
func BenchmarkRecipientSlice_100(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rcpts := make([]string, 0, 100)
		for j := 0; j < 100; j++ {
			rcpts = append(rcpts, "recipient@example.com")
		}
		_ = rcpts
	}
}
