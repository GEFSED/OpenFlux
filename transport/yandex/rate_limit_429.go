package yandex

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Fixed policy for the opt-in Perf Lab experiment; not adaptive concurrency.
const (
	rateLimitFallbackMin   = 25 * time.Millisecond
	rateLimitFallbackMax   = time.Second
	rateLimitRetryAfterMax = 5 * time.Second
)

// Per-instance clock seam lets local HTTP tests advance cooldowns deterministically.
type rateLimitClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type realRateLimitClock struct{}

func (realRateLimitClock) Now() time.Time { return time.Now() }
func (realRateLimitClock) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type VolgaRateLimitDiagnostics struct {
	Enabled           bool   `json:"rate_limit_guard_enabled"`
	Events429         uint64 `json:"rate_limit_429_events"`
	WaitEvents        uint64 `json:"rate_limit_wait_events"`
	WaitNS            int64  `json:"rate_limit_wait_ns"`
	RetryAfterUsed    uint64 `json:"rate_limit_retry_after_used"`
	RetryAfterInvalid uint64 `json:"rate_limit_retry_after_invalid"`
	FallbackUsed      uint64 `json:"rate_limit_fallback_used"`
	GateRemainingNS   int64  `json:"rate_limit_current_gate_remaining_ns"`
	MaxCooldownNS     int64  `json:"rate_limit_max_cooldown_ns"`
}

type relay429Gate struct {
	mu         sync.Mutex
	clock      rateLimitClock
	until      time.Time
	generation uint64
	fallback   time.Duration
	diag       VolgaRateLimitDiagnostics
}

func newRelay429Gate(enabled bool) *relay429Gate {
	if !enabled {
		return nil
	}
	return &relay429Gate{clock: realRateLimitClock{}, diag: VolgaRateLimitDiagnostics{Enabled: true}}
}

// wait runs in the existing worker, creates no goroutine, and holds only the
// gate mutex briefly. An extension is rechecked after the old deadline wakes.
func (g *relay429Gate) wait(ctx context.Context) (uint64, error) {
	var started time.Time
	waited := false
	defer func() {
		if waited {
			elapsed := g.clock.Now().Sub(started)
			g.mu.Lock()
			g.diag.WaitNS += max(elapsed.Nanoseconds(), 0)
			g.mu.Unlock()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		g.mu.Lock()
		now := g.clock.Now()
		remaining := g.until.Sub(now)
		generation := g.generation
		if remaining <= 0 {
			g.mu.Unlock()
			return generation, nil
		}
		if !waited {
			waited, started = true, now
			g.diag.WaitEvents++
		}
		g.mu.Unlock()
		if err := g.clock.Wait(ctx, remaining); err != nil {
			return 0, err
		}
	}
}

// Numeric delta-seconds (including zero) and future HTTP dates are accepted.
// Huge digit-only values cap safely without integer/duration overflow. Past
// dates, malformed/empty values and multiple fields use the bounded fallback.
func retryAfterCooldown(values []string, now time.Time) (time.Duration, bool) {
	if len(values) != 1 {
		return 0, false
	}
	s := strings.TrimSpace(values[0])
	if s == "" {
		return 0, false
	}
	seconds := int64(0)
	digits := true
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			digits = false
			break
		}
		if seconds <= int64(rateLimitRetryAfterMax/time.Second) {
			seconds = seconds*10 + int64(s[i]-'0')
		}
	}
	if digits {
		return min(time.Duration(seconds)*time.Second, rateLimitRetryAfterMax), true
	}
	date, err := http.ParseTime(s)
	if err != nil || date.Before(now) {
		return 0, false
	}
	return min(date.Sub(now), rateLimitRetryAfterMax), true
}

func (g *relay429Gate) rejected(values []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock.Now()
	cooldown, valid := retryAfterCooldown(values, now)
	g.diag.Events429++
	g.generation++
	if valid {
		g.diag.RetryAfterUsed++
	} else {
		if len(values) > 0 {
			g.diag.RetryAfterInvalid++
		}
		if g.fallback == 0 {
			g.fallback = rateLimitFallbackMin
		} else {
			g.fallback = min(g.fallback*2, rateLimitFallbackMax)
		}
		cooldown = g.fallback
		g.diag.FallbackUsed++
	}
	// Concurrent 429s extend, never shorten, the existing deadline.
	if next := now.Add(cooldown); next.After(g.until) {
		g.until = next
	}
	g.diag.MaxCooldownNS = max(g.diag.MaxCooldownNS, cooldown.Nanoseconds())
}

func (g *relay429Gate) succeeded(generation uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// A success from before a later 429 cannot erase its strike state, even
	// if that old request finishes after the new cooldown has expired.
	if generation == g.generation && !g.clock.Now().Before(g.until) {
		g.fallback = 0
	}
}

func (g *relay429Gate) snapshot() VolgaRateLimitDiagnostics {
	g.mu.Lock()
	defer g.mu.Unlock()
	d := g.diag
	d.GateRemainingNS = max(g.until.Sub(g.clock.Now()).Nanoseconds(), 0)
	return d
}
