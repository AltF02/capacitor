package capacitor

import "time"

type Config struct {
	Capacity  int64
	LeakRate  float64
	KeyPrefix string
	Timeout   time.Duration
}

func DefaultConfig() Config {
	return Config{
		Capacity:  10,
		LeakRate:  2, // 2 req/sec leak rate
		KeyPrefix: "ratelimit:leaky",
		Timeout:   50 * time.Millisecond, // Aggressive timeout for fail-fast
	}
}
