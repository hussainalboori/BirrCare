package auth

import (
	"sync"
	"time"
)

// PINLimiter throttles PIN guesses per caregiver. A 4-digit PIN has only 10,000
// combinations, so without throttling it falls in minutes. After MaxFailures
// consecutive failures the account is locked with exponential back-off
// (BaseLockout, doubling, capped at MaxLockout). Attempts are reserved before
// the bcrypt comparison, so a parallel burst cannot exceed the budget.
type PINLimiter struct {
	MaxFailures int
	BaseLockout time.Duration
	MaxLockout  time.Duration
	ResetAfter  time.Duration // forget failures after this much quiet time
	Now         func() time.Time

	mu      sync.Mutex
	entries map[string]*pinEntry
}

type pinEntry struct {
	failures    int
	inFlight    int
	lockedUntil time.Time
	lastFailure time.Time
}

func NewPINLimiter() *PINLimiter {
	return &PINLimiter{
		MaxFailures: 5,
		BaseLockout: 30 * time.Second,
		MaxLockout:  15 * time.Minute,
		ResetAfter:  1 * time.Hour,
		Now:         time.Now,
		entries:     map[string]*pinEntry{},
	}
}

// DefaultPINLimiter is shared by handlers that are not given an explicit limiter.
var DefaultPINLimiter = NewPINLimiter()

// Begin reserves one attempt for key. If it returns ok=false the caller must
// reject the request (HTTP 429) and NOT call Done.
func (l *PINLimiter) Begin(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()

	e := l.entries[key]
	if e == nil {
		e = &pinEntry{}
		l.entries[key] = e
	}
	if e.inFlight == 0 && e.failures > 0 && now.Sub(e.lastFailure) > l.ResetAfter {
		*e = pinEntry{}
	}
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	// Below the threshold, allow up to the remaining budget concurrently; once over
	// it (after a lockout expired) allow exactly one probe at a time.
	if e.failures+e.inFlight >= l.MaxFailures && !(e.failures >= l.MaxFailures && e.inFlight == 0) {
		return false, time.Second
	}
	e.inFlight++
	return true, 0
}

// Done records the outcome of an attempt reserved with Begin.
func (l *PINLimiter) Done(key string, success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		return
	}
	if e.inFlight > 0 {
		e.inFlight--
	}
	if success {
		if e.inFlight == 0 {
			delete(l.entries, key)
		} else {
			e.failures, e.lockedUntil = 0, time.Time{}
		}
		return
	}
	now := l.Now()
	e.failures++
	e.lastFailure = now
	if e.failures >= l.MaxFailures {
		d := l.BaseLockout << uint(min(e.failures-l.MaxFailures, 20))
		if d > l.MaxLockout || d <= 0 {
			d = l.MaxLockout
		}
		e.lockedUntil = now.Add(d)
	}
}
