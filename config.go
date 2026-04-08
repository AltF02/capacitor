package capacitor

import "time"

// Config defines the parameters for a leaky-bucket rate limiter.
type Config struct {
	Capacity  int64         // maximum number of requests the bucket can hold
	LeakRate  float64       // requests drained per second
	KeyPrefix string        // Valkey key prefix for bucket storage
	Timeout   time.Duration // per-operation Valkey timeout
}

// ProfileConfig maps profile names to their rate limit configurations.
// Profile key prefixes are auto-namespaced with ":profile:<name>" to
// prevent collisions. Unknown or empty profile names fall back to the
// default limiter. Used with [WithProfiles] and [WithClassifier].
type ProfileConfig map[string]Config

// DefaultConfig returns a Config with sensible defaults for general use.
func DefaultConfig() Config {
	return Config{
		Capacity:  20,
		LeakRate:  5, // 5 req/sec leak rate
		KeyPrefix: "capacitor",
		Timeout:   50 * time.Millisecond,
	}
}
