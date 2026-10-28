package onesie_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/frodi-karlsson/onesie-go"
)

func TestSystemClock(t *testing.T) {
	t.Parallel()

	t.Run("should read the current time", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			if got := onesie.SystemClock().Now(); !got.Equal(time.Now()) {
				t.Errorf("Now = %v, want %v", got, time.Now())
			}
		})
	})

	t.Run("should sleep for the whole duration", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			started := time.Now()

			if err := onesie.SystemClock().Sleep(t.Context(), 3*time.Second); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if elapsed := time.Since(started); elapsed != 3*time.Second {
				t.Errorf("slept %v, want 3s", elapsed)
			}
		})
	})

	t.Run("should stop sleeping when the context ends", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			started := time.Now()

			if err := onesie.SystemClock().Sleep(ctx, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want context.DeadlineExceeded", err)
			}

			if elapsed := time.Since(started); elapsed != time.Second {
				t.Errorf("slept %v, want 1s", elapsed)
			}
		})
	})

	t.Run("should be a clock WithClock takes", func(t *testing.T) {
		t.Parallel()

		if _, err := onesie.New(onesie.WithAPIKey("k"), onesie.WithClock(onesie.SystemClock())); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
