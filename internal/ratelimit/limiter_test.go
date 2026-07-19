package ratelimit

import (
	"testing"
	"time"
)

func TestAllowTripsAfterMax(t *testing.T) {
	l := New(3, time.Minute)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return base }

	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("attempt %d should allow", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatal("4th should deny")
	}
	if !l.Allow("other") {
		t.Fatal("other key should allow")
	}
}

func TestWindowResets(t *testing.T) {
	l := New(1, time.Minute)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cur := base
	l.now = func() time.Time { return cur }

	if !l.Allow("k") {
		t.Fatal("first")
	}
	if l.Allow("k") {
		t.Fatal("second in window")
	}
	cur = base.Add(time.Minute)
	if !l.Allow("k") {
		t.Fatal("after window")
	}
}

func TestDisabledWhenMaxZero(t *testing.T) {
	l := New(0, time.Minute)
	for i := 0; i < 100; i++ {
		if !l.Allow("k") {
			t.Fatal("disabled limiter must always allow")
		}
	}
}
