package capacitor_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"unsafe"

	"codeberg.org/matthew/capacitor"

	"github.com/google/go-cmp/cmp"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestAttempt(t *testing.T) {
	cfg := capacitor.DefaultConfig()

	cases := map[string]struct {
		uid            string
		allowed        bool
		remaining      int
		mockValkey     bool
		expectedResult capacitor.Result
		expectedErr    error
	}{
		"empty uid returns error": {
			uid:            "",
			mockValkey:     false,
			expectedResult: capacitor.Result{},
			expectedErr:    capacitor.ErrEmptyUID,
		},
		"request allowed": {
			uid:        "user:1",
			allowed:    true,
			remaining:  9,
			mockValkey: true,
			expectedResult: capacitor.Result{
				Allowed:   true,
				Remaining: 9,
				Limit:     10,
			},
		},
		"request denied": {
			uid:        "user:1",
			allowed:    false,
			remaining:  0,
			mockValkey: true,
			expectedResult: capacitor.Result{
				Allowed:    false,
				Remaining:  0,
				Limit:      10,
				RetryAfter: 1 * time.Second,
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
						mock.ValkeyInt64(int64(*(*byte)(unsafe.Pointer(&c.allowed)))),
						mock.ValkeyInt64(int64(c.remaining)),
					)))
			}

			s := capacitor.New(client, cfg)
			actualRes, err := s.Attempt(context.Background(), c.uid)

			if !errors.Is(err, c.expectedErr) {
				t.Fatalf("Attempt() error; got = %v, want = %v", err, c.expectedErr)
			}

			if diff := cmp.Diff(c.expectedResult, actualRes); diff != "" {
				t.Errorf("capacitor.Result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
