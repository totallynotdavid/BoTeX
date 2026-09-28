package ratelimit_test

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/totallynotdavid/botkit/internal/ratelimit"
)

const (
	period   = time.Minute
	cooldown = 5 * time.Minute
)

// newLimiter builds a limiter with the package's test cooldown.
func newLimiter(t *testing.T, requests int, period time.Duration) *ratelimit.Limiter {
	t.Helper()

	limiter, err := ratelimit.NewLimiter(requests, period, cooldown)
	if err != nil {
		t.Fatal(err)
	}

	return limiter
}

func TestNewLimiterRejectsLimitsThatCannotAdmitAnyone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		requests int
		period   time.Duration
	}{
		{"no requests", 0, period},
		{"negative requests", -1, period},
		{"no period", 1, 0},
		{"negative period", 1, -time.Second},
	}

	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			limiter, err := ratelimit.NewLimiter(scenario.requests, scenario.period, cooldown)
			if !errors.Is(err, ratelimit.ErrInvalidLimit) {
				t.Fatalf("NewLimiter = %v, %v, want ErrInvalidLimit", limiter, err)
			}
		})
	}
}

func TestLimiterAllowsUpToTheLimitPerWindow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 2, period)

		for i := range 2 {
			if !limiter.Check("a").Allowed {
				t.Fatalf("request %d denied", i)
			}
		}

		denied := limiter.Check("a")
		if denied.Allowed {
			t.Fatal("third request allowed")
		}

		if denied.ResetAfter != period {
			t.Errorf("ResetAfter = %v, want %v", denied.ResetAfter, period)
		}

		time.Sleep(period)

		if !limiter.Check("a").Allowed {
			t.Error("request denied after the window passed")
		}
	})
}

func TestLimiterResetAfterShrinksAsTheWindowMoves(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 1, period)
		limiter.Check("a")

		time.Sleep(20 * time.Second)

		got := limiter.Check("a").ResetAfter
		if want := 40 * time.Second; got != want {
			t.Errorf("ResetAfter = %v, want %v", got, want)
		}
	})
}

func TestLimiterCountsUsersSeparately(t *testing.T) {
	t.Parallel()

	limiter := newLimiter(t, 1, period)

	if !limiter.Check("a").Allowed || !limiter.Check("b").Allowed {
		t.Fatal("first request of each user must be allowed")
	}

	if limiter.Check("a").Allowed {
		t.Error("second request from a allowed")
	}
}

func TestLimiterDoesNotCountDeniedRequests(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 1, period)
		limiter.Check("a")

		for range 9 {
			time.Sleep(period / 10)
			limiter.Check("a")
		}

		time.Sleep(period / 10)

		if !limiter.Check("a").Allowed {
			t.Error("denied requests extended the window")
		}
	})
}

func TestLimiterNotifiesOncePerCooldown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 1, period)
		limiter.Check("a")

		if !limiter.Check("a").Notify {
			t.Fatal("first denial does not ask for a notice")
		}

		if limiter.Check("a").Notify {
			t.Error("second denial in the cooldown asks for a notice")
		}
	})
}

func TestLimiterNotifiesAgainAfterCooldown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 1, 10*time.Minute)
		limiter.Check("a")
		limiter.Check("a")

		time.Sleep(cooldown)

		if !limiter.Check("a").Notify {
			t.Error("denial after the cooldown does not ask for a notice")
		}
	})
}

func TestLimiterNotifiesAgainAfterAnAllowedRequest(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		limiter := newLimiter(t, 1, period)
		limiter.Check("a")
		limiter.Check("a")

		time.Sleep(period)
		limiter.Check("a")

		if !limiter.Check("a").Notify {
			t.Error("a new burst after an allowed request gets no notice")
		}
	})
}
