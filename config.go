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
		Capacity:  20,
		LeakRate:  5, // 5 req/sec leak rate
		KeyPrefix: "capacitor",
		Timeout:   50 * time.Millisecond,
	}
}
