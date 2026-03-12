# gRPC Interceptor

Rate limiting for gRPC services.

```go
package myapp

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"codeberg.org/matthew/capacitor"
)

type RateLimiter struct {
	limiter *capacitor.Capacitor
}

func NewRateLimiter(store capacitor.Store, cfg capacitor.Config) *RateLimiter {
	return &RateLimiter{
		limiter: capacitor.New(store, cfg),
	}
}

// UnaryInterceptor returns a gRPC unary interceptor
func (r *RateLimiter) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract key from metadata
		key := extractKeyFromContext(ctx)
		if key == "" {
			return handler(ctx, req)
		}

		result, err := r.limiter.Attempt(ctx, key)
		if err != nil {
			// On error, you can fail open or closed
			return handler(ctx, req)
		}

		if !result.Allowed {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}

		return handler(ctx, req)
	}
}

func extractKeyFromContext(ctx context.Context) string {
	// Extract from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	// Try user ID
	if v := md.Get("x-user-id"); len(v) > 0 {
		return v[0]
	}

	// Try API key
	if v := md.Get("x-api-key"); len(v) > 0 {
		return v[0]
	}

	return ""
}

// Usage
func main() {
	store := capacitor_test.NewMockStore(100, 10)
	limiter := NewRateLimiter(store, capacitor.DefaultConfig())

	server := grpc.NewServer(
		grpc.UnaryInterceptor(limiter.UnaryInterceptor()),
	)
}
```
