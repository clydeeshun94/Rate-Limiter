package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func BenchmarkFixedWindow_Check(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Check("bench", policy)
	}
}

func BenchmarkSlidingWindowCounter_Check(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Check("bench", policy)
	}
}

func BenchmarkTokenBucket_Check(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewTokenBucketWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Check("bench", policy)
	}
}

func BenchmarkLeakyBucket_Check(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewLeakyBucketWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Check("bench", policy)
	}
}

func BenchmarkSlidingWindowLog_Check(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Check("bench", policy)
	}
}

func BenchmarkFixedWindow_Concurrent(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Check("bench", policy)
		}
	})
}

func BenchmarkSlidingWindowLog_Concurrent(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Check("bench", policy)
		}
	})
}

func BenchmarkFixedWindow_MemoryPerIdentity(b *testing.B) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		identity := string(rune('a' + i%26)) + string(rune('A' + i%26))
		l.Check(identity, policy)
	}
}
