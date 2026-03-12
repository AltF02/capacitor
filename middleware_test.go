package capacitor_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"codeberg.org/matthew/capacitor"

	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestKeyFromRemoteIP(t *testing.T) {
	cases := map[string]struct {
		remoteAddr string
		expected   string
	}{
		"ipv4 with port": {
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		"ipv4 without port": {
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
		"ipv6 with port": {
			remoteAddr: "[::1]:12345",
			expected:   "::1",
		},
		"ipv6 without port": {
			remoteAddr: "::1",
			expected:   "::1",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = c.remoteAddr

			got := capacitor.KeyFromRemoteIP(r)
			if got != c.expected {
				t.Errorf("KeyFromRemoteIP() = %q, want %q", got, c.expected)
			}
		})
	}
}

func TestKeyFromHeader(t *testing.T) {
	cases := map[string]struct {
		header   string
		value    string
		expected string
	}{
		"header present": {
			header:   "X-API-Key",
			value:    "abc123",
			expected: "abc123",
		},
		"header missing": {
			header:   "X-API-Key",
			value:    "",
			expected: "",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.value != "" {
				r.Header.Set(c.header, c.value)
			}

			got := capacitor.KeyFromHeader(c.header)(r)
			if got != c.expected {
				t.Errorf("KeyFromHeader(%q) = %q, want %q", c.header, got, c.expected)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	cfg := capacitor.DefaultConfig()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	cases := map[string]struct {
		allowed         bool
		remaining       int
		remoteAddr      string
		mockValkey      bool
		opts            []capacitor.MiddlewareOption
		expectedStatus  int
		expectedBody    string
		expectedHeaders map[string]string
	}{
		"allowed request passes through": {
			allowed:        true,
			remaining:      9,
			remoteAddr:     "10.0.0.1:1234",
			mockValkey:     true,
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
			expectedHeaders: map[string]string{
				"RateLimit-Limit":     "20",
				"RateLimit-Remaining": "9",
			},
		},
		"denied request returns 429": {
			allowed:        false,
			remaining:      0,
			remoteAddr:     "10.0.0.1:1234",
			mockValkey:     true,
			expectedStatus: http.StatusTooManyRequests,
			expectedBody:   "Too Many Requests\n",
			expectedHeaders: map[string]string{
				"RateLimit-Limit":     "20",
				"RateLimit-Remaining": "0",
				"RateLimit-Reset":     "1",
				"Retry-After":         "1",
			},
		},
		"empty key skips rate limiting": {
			remoteAddr: "10.0.0.1:1234",
			mockValkey: false,
			opts: []capacitor.MiddlewareOption{
				capacitor.WithKeyFunc(func(_ *http.Request) string { return "" }),
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
		},
		"custom deny handler": {
			allowed:    false,
			remaining:  0,
			remoteAddr: "10.0.0.1:1234",
			mockValkey: true,
			opts: []capacitor.MiddlewareOption{
				capacitor.WithDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"error":"rate limited"}`))
				})),
			},
			expectedStatus: http.StatusTooManyRequests,
			expectedBody:   `{"error":"rate limited"}`,
			expectedHeaders: map[string]string{
				"Content-Type": "application/json",
			},
		},
		"custom key func": {
			allowed:    true,
			remaining:  5,
			remoteAddr: "10.0.0.1:1234",
			mockValkey: true,
			opts: []capacitor.MiddlewareOption{
				capacitor.WithKeyFunc(capacitor.KeyFromHeader("X-API-Key")),
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "OK",
			expectedHeaders: map[string]string{
				"RateLimit-Remaining": "5",
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := mock.NewClient(ctrl)

			if c.mockValkey {
				client.EXPECT().
					Do(gomock.Any(), gomock.Any()).
					Return(mock.Result(mock.ValkeyArray(
						mock.ValkeyInt64(btoi(c.allowed)),
						mock.ValkeyInt64(int64(c.remaining)),
					)))
			}

			store := capacitor.NewValkeyStore(client, cfg)
			limiter := capacitor.New(store, cfg, capacitor.WithLogger(slog.Default()))
			handler := capacitor.NewMiddleware(limiter, c.opts...)(next)

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = c.remoteAddr
			if name == "custom key func" {
				req.Header.Set("X-API-Key", "test-key")
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != c.expectedStatus {
				t.Errorf("status = %d, want %d", rec.Code, c.expectedStatus)
			}
			if got := rec.Body.String(); got != c.expectedBody {
				t.Errorf("body = %q, want %q", got, c.expectedBody)
			}
			for k, want := range c.expectedHeaders {
				if got := rec.Header().Get(k); got != want {
					t.Errorf("header %q = %q, want %q", k, got, want)
				}
			}
		})
	}
}
