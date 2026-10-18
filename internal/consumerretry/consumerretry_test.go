package consumerretry

import (
	"errors"
	"testing"
	"time"
)

// fakeClock 让退避与总耗时判断在测试里完全确定，不依赖真实时间。
type fakeClock struct {
	current time.Time
	slept   []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{current: time.Unix(0, 0)}
}

func (c *fakeClock) now() time.Time { return c.current }

func (c *fakeClock) sleep(d time.Duration) {
	c.slept = append(c.slept, d)
	c.current = c.current.Add(d)
}

func retryable(err error) (string, bool) { return "mysql_persist", true }

func nonRetryable(err error) (string, bool) { return "decode", false }

func defaultPolicy() Policy {
	return Policy{
		MaxRetries:   5,
		BaseBackoff:  200 * time.Millisecond,
		MaxBackoff:   3200 * time.Millisecond,
		MaxTotalTime: 10 * time.Second,
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	policy := defaultPolicy()
	want := []time.Duration{
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
		3200 * time.Millisecond,
	}
	for i, wantBackoff := range want {
		failures := i + 1
		backoff, allowed := policy.BackoffAfterFailures(failures, 0)
		if !allowed {
			t.Fatalf("failures=%d should still be allowed", failures)
		}
		if backoff != wantBackoff {
			t.Fatalf("failures=%d backoff=%v want %v", failures, backoff, wantBackoff)
		}
	}
	// 第 MaxRetries+1 次失败已经超出预算。
	if _, allowed := policy.BackoffAfterFailures(6, 0); allowed {
		t.Fatalf("failures=6 should exhaust the budget")
	}
}

func TestBackoffCappedByMaxBackoff(t *testing.T) {
	policy := Policy{MaxRetries: 10, BaseBackoff: time.Second, MaxBackoff: 2 * time.Second}
	for failures := 1; failures <= 10; failures++ {
		backoff, allowed := policy.BackoffAfterFailures(failures, 0)
		if !allowed {
			t.Fatalf("failures=%d should be allowed", failures)
		}
		if backoff > 2*time.Second {
			t.Fatalf("failures=%d backoff=%v exceeds cap", failures, backoff)
		}
	}
}

func TestDisabledRetryExhaustsImmediately(t *testing.T) {
	policy := Policy{MaxRetries: 0, BaseBackoff: time.Millisecond}
	if _, allowed := policy.BackoffAfterFailures(1, 0); allowed {
		t.Fatalf("MaxRetries=0 must not allow any retry")
	}
}

func TestRunSucceedsOnFirstAttempt(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	outcome, _, err, attempts := Run(defaultPolicy(), func() error {
		calls++
		return nil
	}, retryable, nil, clock.now, clock.sleep)

	if outcome != OutcomeSucceeded || err != nil {
		t.Fatalf("outcome=%v err=%v", outcome, err)
	}
	if attempts != 1 || calls != 1 {
		t.Fatalf("attempts=%d calls=%d want 1", attempts, calls)
	}
	if len(clock.slept) != 0 {
		t.Fatalf("unexpected sleeps: %v", clock.slept)
	}
}

func TestRunRetriesThenSucceeds(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	outcome, _, err, attempts := Run(defaultPolicy(), func() error {
		calls++
		if calls <= 2 {
			return errors.New("boom")
		}
		return nil
	}, retryable, nil, clock.now, clock.sleep)

	if outcome != OutcomeSucceeded || err != nil {
		t.Fatalf("outcome=%v err=%v", outcome, err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d want 3", attempts)
	}
	want := []time.Duration{200 * time.Millisecond, 400 * time.Millisecond}
	if len(clock.slept) != len(want) {
		t.Fatalf("slept=%v want %v", clock.slept, want)
	}
	for i := range want {
		if clock.slept[i] != want[i] {
			t.Fatalf("slept[%d]=%v want %v", i, clock.slept[i], want[i])
		}
	}
}

func TestRunExhaustsBudget(t *testing.T) {
	clock := newFakeClock()
	outcome, stage, err, attempts := Run(defaultPolicy(), func() error {
		return errors.New("always fails")
	}, retryable, nil, clock.now, clock.sleep)

	if outcome != OutcomeExhausted {
		t.Fatalf("outcome=%v want exhausted", outcome)
	}
	if err == nil || stage != "mysql_persist" {
		t.Fatalf("err=%v stage=%q", err, stage)
	}
	// 首次尝试 + 5 次重试 = 6 次尝试，5 次退避。
	if attempts != 6 {
		t.Fatalf("attempts=%d want 6", attempts)
	}
	if len(clock.slept) != 5 {
		t.Fatalf("slept=%v want 5 sleeps", clock.slept)
	}
}

func TestRunStopsOnNonRetryable(t *testing.T) {
	clock := newFakeClock()
	outcome, stage, _, attempts := Run(defaultPolicy(), func() error {
		return errors.New("bad payload")
	}, nonRetryable, nil, clock.now, clock.sleep)

	if outcome != OutcomeNonRetryable || stage != "decode" || attempts != 1 {
		t.Fatalf("outcome=%v stage=%q attempts=%d", outcome, stage, attempts)
	}
	if len(clock.slept) != 0 {
		t.Fatalf("non retryable must not sleep, got %v", clock.slept)
	}
}

func TestRunStopsWhenShuttingDown(t *testing.T) {
	clock := newFakeClock()
	outcome, _, _, attempts := Run(defaultPolicy(), func() error {
		return errors.New("boom")
	}, retryable, func() bool { return true }, clock.now, clock.sleep)

	if outcome != OutcomeStopped {
		t.Fatalf("outcome=%v want stopped", outcome)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1", attempts)
	}
	if len(clock.slept) != 0 {
		t.Fatalf("shutdown must not sleep, got %v", clock.slept)
	}
}

func TestRunRespectsMaxTotalTime(t *testing.T) {
	// 退避 200/400/800 累计 1400ms > 1000ms 上限，所以第三次失败后必须放弃。
	policy := Policy{
		MaxRetries:   100,
		BaseBackoff:  200 * time.Millisecond,
		MaxBackoff:   time.Second,
		MaxTotalTime: time.Second,
	}
	clock := newFakeClock()
	outcome, _, _, attempts := Run(policy, func() error {
		return errors.New("always fails")
	}, retryable, nil, clock.now, clock.sleep)

	if outcome != OutcomeExhausted {
		t.Fatalf("outcome=%v want exhausted", outcome)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d want 3 (200+400+800 exceeds 1000ms)", attempts)
	}
}
