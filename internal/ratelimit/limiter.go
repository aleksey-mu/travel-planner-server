package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a crude fixed-window counter keyed by arbitrary strings (IP, identity, …).
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	buckets map[string]*bucket
	ops     uint64
	now     func() time.Time
}

type bucket struct {
	count int
	start time.Time
}

// New returns a limiter allowing max events per window. max <= 0 disables limiting (always allow).
func New(max int, window time.Duration) *Limiter {
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		max:     max,
		window:  window,
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

// Allow records one attempt for key and reports whether it is within the limit.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	if key == "" {
		key = "_"
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.ops++
	if l.ops%64 == 0 {
		l.gcExpiredLocked(now)
	}

	b, ok := l.buckets[key]
	if !ok || now.Sub(b.start) >= l.window {
		l.buckets[key] = &bucket{count: 1, start: now}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// Reset clears all buckets (tests).
func (l *Limiter) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets = make(map[string]*bucket)
}

func (l *Limiter) gcExpiredLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.start) >= l.window {
			delete(l.buckets, k)
		}
	}
}
