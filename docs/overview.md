# Overview

Capacitor is a leaky-bucket rate limiter for Go designed for distributed systems. It provides atomic rate limiting across multiple application instances by delegating the bucket state to a backend store.

## Key Concepts

### Leaky Bucket Algorithm

The leaky bucket algorithm models a bucket with a finite capacity that leaks at a constant rate:

```
         Requests          Allowed
            │               │
            ▼               │
    ┌───────────────┐       │
    │    Bucket     │───────┼───► Process request
    │  (capacity)  │       │
    └───────────────┘       │
            │               │
            ▼               ▼
         Leak (tokens/second)
```

- **Capacity**: Maximum tokens in the bucket
- **LeakRate**: Tokens removed per second
- When a request arrives: if tokens available, decrement and allow; otherwise deny

### Store Interface

Capacitor uses a `Store` interface, allowing different backend implementations:

```go
type Store interface {
    Attempt(ctx context.Context, uid string) (Result, error)
    HealthCheck(ctx context.Context) error
    Close() error
}
```

## Features

- **Atomic operations**: Bucket state updates happen in a single atomic operation
- **Distributed safe**: Works correctly across multiple application instances
- **Multiple backends**: Valkey (Redis-compatible) or PostgreSQL
- **IETF headers**: Standard `RateLimit-*` headers on responses
- **Fallback strategies**: Fail-open or fail-closed when backend is unreachable
- **Flexible key extraction**: Rate limit by IP, header, or custom logic

## Use Cases

- API rate limiting
- Protecting against brute-force attacks
- Throttling background jobs
- Rate limiting external API calls
