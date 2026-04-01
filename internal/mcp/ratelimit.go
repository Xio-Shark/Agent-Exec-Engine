package mcp

import (
	"sync"
	"time"
)

// RateLimiter provides per-tool token bucket rate limiting.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	limits  map[string]int // tool name → max calls per minute
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		limits:  make(map[string]int),
	}
}

// SetLimit configures the rate limit for a tool (calls per minute).
func (rl *RateLimiter) SetLimit(toolName string, callsPerMinute int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.limits[toolName] = callsPerMinute
	rl.buckets[toolName] = &tokenBucket{
		tokens:     float64(callsPerMinute),
		maxTokens:  float64(callsPerMinute),
		refillRate: float64(callsPerMinute) / 60.0,
		lastRefill: time.Now(),
	}
}

// Allow checks if a tool call is allowed under its rate limit.
// Returns true if no limit is configured.
func (rl *RateLimiter) Allow(toolName string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.buckets[toolName]
	if !ok {
		return true // no limit configured
	}

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokens += elapsed * bucket.refillRate
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
	bucket.lastRefill = now

	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}
	return false
}
