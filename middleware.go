package capacitor

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
)

// KeyFunc extracts the rate-limit key from an incoming request.
type KeyFunc func(r *http.Request) string

// ClassifyFunc determines the rate-limit profile name for a request.
// An empty return value uses the default limiter.
type ClassifyFunc func(r *http.Request) string

// MiddlewareOption configures the HTTP middleware.
type MiddlewareOption func(*middleware)

type middleware struct {
	limiter     *Capacitor
	keyFunc     KeyFunc
	denyHandler http.Handler
	profiles    map[string]*Capacitor
	classifier  ClassifyFunc
}

// WithKeyFunc sets the function used to derive the rate-limit key.
// Defaults to KeyFromRemoteIP.
func WithKeyFunc(fn KeyFunc) MiddlewareOption {
	return func(m *middleware) { m.keyFunc = fn }
}

// WithDenyHandler replaces the default 429 response handler.
func WithDenyHandler(h http.Handler) MiddlewareOption {
	return func(m *middleware) { m.denyHandler = h }
}

// WithProfiles configures per-profile limiters that share the base
// limiter's Valkey client. Profile key prefixes are auto-namespaced
// with ":profile:<name>" to prevent collisions. Unknown or empty
// profile names fall back to the default limiter.
func WithProfiles(profiles ProfileConfig) MiddlewareOption {
	return func(m *middleware) { m.buildProfiles(profiles) }
}

// WithClassifier sets the function used to route a request to a
// named rate-limit profile. See [WithProfiles].
func WithClassifier(fn ClassifyFunc) MiddlewareOption {
	return func(m *middleware) { m.classifier = fn }
}

// buildProfiles creates per-profile limiters that share the base
// limiter's Valkey client and settings.
func (m *middleware) buildProfiles(profiles ProfileConfig) {
	if len(profiles) == 0 {
		return
	}

	m.profiles = make(map[string]*Capacitor, len(profiles))

	for name, cfg := range profiles {
		cfg.KeyPrefix += ":profile:" + name
		m.profiles[name] = &Capacitor{
			client:   m.limiter.client,
			config:   cfg,
			logger:   m.limiter.logger,
			fallback: m.limiter.fallback,
			metrics:  m.limiter.metrics,
		}
	}
}

// resolve returns the limiter and profile name for the request.
func (m *middleware) resolve(r *http.Request) (*Capacitor, string) {
	if m.classifier == nil {
		return m.limiter, ""
	}
	name := m.classifier(r)
	if name == "" {
		return m.limiter, ""
	}
	if limiter, ok := m.profiles[name]; ok {
		return limiter, name
	}
	return m.limiter, ""
}

// KeyFromRemoteIP extracts the IP from RemoteAddr, stripping the port.
func KeyFromRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // already bare IP
	}
	return host
}

// KeyFromHeader returns a KeyFunc that reads the given header.
func KeyFromHeader(name string) KeyFunc {
	return func(r *http.Request) string {
		return r.Header.Get(name)
	}
}

// NewMiddleware returns standard net/http middleware that rate-limits
// requests using the provided Capacitor instance.
func NewMiddleware(limiter *Capacitor, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	m := &middleware{
		limiter:     limiter,
		keyFunc:     KeyFromRemoteIP,
		denyHandler: http.HandlerFunc(defaultDeny),
	}

	for _, opt := range opts {
		opt(m)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := m.keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			limiter, profile := m.resolve(r)

			result, err := limiter.Attempt(r.Context(), key)
			if err != nil {
				limiter.logger.Warn("rate limiter degraded, using fallback",
					"error", err, "key", key, "profile", profile)
			}

			result.writeHeaders(w)

			if !result.Allowed {
				m.denyHandler.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeHeaders sets IETF RateLimit-* headers on the response.
func (r Result) writeHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("RateLimit-Limit", strconv.FormatInt(r.Limit, 10))
	h.Set("RateLimit-Remaining", strconv.FormatInt(r.Remaining, 10))

	if r.RetryAfter > 0 {
		secs := strconv.FormatInt(int64(r.RetryAfter.Seconds()), 10)
		h.Set("RateLimit-Reset", secs)
		h.Set("Retry-After", secs)
	}
}

func defaultDeny(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = fmt.Fprintln(w, http.StatusText(http.StatusTooManyRequests))
}
