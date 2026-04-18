// Package ratelimit provides per-IP rate limiting for HTTP servers.
//
// The Limiter interface is deliberately minimal so that alternative backends
// (e.g. Redis) can be swapped in without changing call-sites.
package ratelimit

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Limiter reports whether the next request from ip is within the rate limits.
// It returns false if the request should be rejected (HTTP 429).
type Limiter interface {
	Allow(ip string) bool
}

// Config holds the rate limit parameters used by NewMemory.
type Config struct {
	// RequestsPerSecond is the burst cap per rolling second window.
	// 0 means no per-second limit.
	RequestsPerSecond int
	// RequestsPerMinute is the sustained cap per rolling minute window.
	// 0 means no per-minute limit.
	RequestsPerMinute int
}

// NewMemory returns an in-memory Limiter that uses sliding-window counters.
// A background goroutine evicts idle IP entries every five minutes to prevent
// unbounded memory growth.
func NewMemory(cfg Config) Limiter {
	m := &memLimiter{
		cfg: cfg,
		ips: make(map[string]*ipState),
	}
	go m.janitor()
	return m
}

// ── Internal types ────────────────────────────────────────────────────────────

type ipState struct {
	mu       sync.Mutex
	secondTs []time.Time // timestamps of requests in the last second
	minuteTs []time.Time // timestamps of requests in the last minute
}

type memLimiter struct {
	cfg Config
	mu  sync.RWMutex
	ips map[string]*ipState
}

func (m *memLimiter) state(ip string) *ipState {
	m.mu.RLock()
	s, ok := m.ips[ip]
	m.mu.RUnlock()
	if ok {
		return s
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok = m.ips[ip]; ok { // double-checked
		return s
	}
	s = &ipState{}
	m.ips[ip] = s
	return s
}

func (m *memLimiter) Allow(ip string) bool {
	s := m.state(ip)
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.secondTs = pruneOlderThan(s.secondTs, now.Add(-time.Second))
	s.minuteTs = pruneOlderThan(s.minuteTs, now.Add(-time.Minute))

	if m.cfg.RequestsPerSecond > 0 && len(s.secondTs) >= m.cfg.RequestsPerSecond {
		slog.Warn("rate limit exceeded", "ip", ip, "limit", "per_second",
			"count", len(s.secondTs), "limit_value", m.cfg.RequestsPerSecond,
			"minute_count", len(s.minuteTs), "minute_limit", m.cfg.RequestsPerMinute)
		return false
	}
	if m.cfg.RequestsPerMinute > 0 && len(s.minuteTs) >= m.cfg.RequestsPerMinute {
		slog.Warn("rate limit exceeded", "ip", ip, "limit", "per_minute",
			"count", len(s.minuteTs), "limit_value", m.cfg.RequestsPerMinute,
			"second_count", len(s.secondTs), "second_limit", m.cfg.RequestsPerSecond)
		return false
	}

	s.secondTs = append(s.secondTs, now)
	s.minuteTs = append(s.minuteTs, now)
	return true
}

// pruneOlderThan removes timestamps from the front of ts that are before cutoff.
func pruneOlderThan(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	return ts[i:]
}

// janitor evicts IPs that have had no traffic in the last minute.
func (m *memLimiter) janitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.evictIdle()
	}
}

func (m *memLimiter) evictIdle() {
	cutoff := time.Now().Add(-time.Minute)
	m.mu.Lock()
	defer m.mu.Unlock()
	for ip, s := range m.ips {
		s.mu.Lock()
		idle := len(s.minuteTs) == 0 || s.minuteTs[len(s.minuteTs)-1].Before(cutoff)
		s.mu.Unlock()
		if idle {
			delete(m.ips, ip)
		}
	}
}

// ── HTTP middleware ────────────────────────────────────────────────────────────

// Middleware returns an HTTP middleware that calls l.Allow with the request's
// RemoteAddr IP and responds with 429 Too Many Requests when denied.
func Middleware(l Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteIP(r.RemoteAddr)
			if !l.Allow(ip) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // already a bare IP (set by realIPMiddleware)
	}
	return host
}
