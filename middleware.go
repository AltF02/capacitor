package capacitor

import (
	"net"
	"net/http"
	"strconv"
)

// KeyFunc extracts the rate-limit key from an incoming request.
type KeyFunc func(r *http.Request) string

// ProfileFunc extracts the rate-limit profile from an incoming request.
type ProfileFunc func(r *http.Request) string

// MiddlewareOption configures the HTTP middleware.
type MiddlewareOption func(*mw)

type mw struct {
	limiter     *Capacitor
	keyFunc     KeyFunc
	denyHandler http.Handler
	profiles    map[string]*Capacitor
	profileFunc ProfileFunc
}

// WithKeyFunc sets the function used to derive the rate-limit key.
// Defaults to KeyFromRemoteIP.
func WithKeyFunc(fn KeyFunc) MiddlewareOption {
	return func(m *mw) { m.keyFunc = fn }
}

// WithDenyHandler replaces the default 429 response handler.
func WithDenyHandler(h http.Handler) MiddlewareOption {
	return func(m *mw) { m.denyHandler = h }
}

// WithProfiles configures per-profile limiters that share the base
// limiter's Valkey client. Closing the base limiter closes all profiles.
// Profile key prefixes are auto-namespaced with ":profile:<name>" to
// prevent collisions. Unknown or empty profile names fall back to the
// default limiter.
func WithProfiles(profiles ProfileConfig) MiddlewareOption {
	return func(m *mw) { m.profiles = makeProfileLimiters(m.limiter, profiles) }
}

// WithProfileFunc sets the function used to derive the rate-limit profile from a request.
func WithProfileFunc(fn ProfileFunc) MiddlewareOption {
	return func(m *mw) { m.profileFunc = fn }
}

func makeProfileLimiters(base *Capacitor, profiles ProfileConfig) map[string]*Capacitor {
	if len(profiles) == 0 {
		return nil
	}

	limiters := make(map[string]*Capacitor)

	for name, cfg := range profiles {
		profileCfg := cfg
		profileCfg.KeyPrefix = cfg.KeyPrefix + ":profile:" + name

		limiter := &Capacitor{
			client:   base.client,
			config:   profileCfg,
			logger:   base.logger,
			fallback: base.fallback,
			metrics:  base.metrics,
		}

		limiters[name] = limiter
	}

	return limiters
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
// requests using the provided capacitor instance.
func NewMiddleware(limiter *Capacitor, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	m := &mw{
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

			selected := m.limiter
			profileName := ""
			if m.profileFunc != nil {
				if p := m.profileFunc(r); p != "" {
					if l, ok := m.profiles[p]; ok {
						selected = l
						profileName = p
					}
				}
			}

			result, err := selected.Attempt(r.Context(), key)
			if err != nil {
				selected.logger.Warn("rate limiter degraded, using fallback",
					"error", err, "key", key, "profile", profileName)
			}

			writeHeaders(w, result)

			if !result.Allowed {
				m.denyHandler.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeHeaders(w http.ResponseWriter, r Result) {
	h := w.Header()
	h.Set("RateLimit-Limit", strconv.FormatInt(r.Limit, 10))
	h.Set("RateLimit-Remaining", strconv.FormatInt(r.Remaining, 10))

	if r.RetryAfter > 0 {
		secs := strconv.FormatInt(int64(r.RetryAfter.Seconds()), 10)
		h.Set("RateLimit-Reset", secs)
		h.Set("Retry-After", secs)
	}
}

func defaultDeny(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte("Too Many Requests\n"))
}
