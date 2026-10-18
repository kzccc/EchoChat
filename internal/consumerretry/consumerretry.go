// Package consumerretry 实现消费侧可重试失败的重试预算治理。
//
// 这个包只做纯决策，不依赖配置、数据库、Kafka 或日志，因此可以直接被单测覆盖。
// 上层消费路径负责把"处理一条消息"包装成 process 回调、把错误分类包装成 classify，
// 再根据返回的 Outcome 决定是否推进位点。
package consumerretry

import "time"

// Outcome 描述一条消息在一次消费周期内的归宿。
//
// 除 OutcomeStopped 之外的三种归宿都表示"这条消息已经有了了结"，
// 调用方可以标记位点让它越过这条消息；只有 OutcomeStopped 表示还没走完，
// 位点必须保持不动，否则这条消息会永久丢失。
type Outcome int

const (
	// OutcomeSucceeded 表示处理成功。
	OutcomeSucceeded Outcome = iota
	// OutcomeNonRetryable 表示失败且被判定为不可重试。
	OutcomeNonRetryable
	// OutcomeExhausted 表示可重试失败已经耗尽重试预算，按不可重试处置。
	OutcomeExhausted
	// OutcomeStopped 表示重试过程中服务开始停机，位点不能推进。
	OutcomeStopped
)

// String 便于日志与测试断言。
func (o Outcome) String() string {
	switch o {
	case OutcomeSucceeded:
		return "succeeded"
	case OutcomeNonRetryable:
		return "non_retryable"
	case OutcomeExhausted:
		return "exhausted"
	case OutcomeStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Policy 是可重试失败的重试预算。
type Policy struct {
	// MaxRetries 是首次尝试之外还允许的重试次数；小于等于 0 表示关闭重试。
	MaxRetries int
	// BaseBackoff 是第一次重试前的退避时长，之后逐次翻倍。
	BaseBackoff time.Duration
	// MaxBackoff 是单次退避的时长上限。
	MaxBackoff time.Duration
	// MaxTotalTime 是一条消息重试加退避的总耗时上限；小于等于 0 表示不限制。
	MaxTotalTime time.Duration
}

// ClassifyFunc 把错误映射成"阶段名 + 是否可重试"。
type ClassifyFunc func(error) (stage string, retryable bool)

const defaultBaseBackoff = 200 * time.Millisecond

// BackoffAfterFailures 在第 failures 次失败之后判断预算是否还允许再试一次。
//
// 返回的 allowed 为 false 表示预算耗尽，调用方应当把这条消息降级为不可重试；
// 返回的 backoff 是在下一次尝试之前应当等待的时长。
func (p Policy) BackoffAfterFailures(failures int, elapsed time.Duration) (backoff time.Duration, allowed bool) {
	if failures <= 0 || p.MaxRetries <= 0 {
		return 0, false
	}
	// 首次尝试之外只允许 MaxRetries 次重试。
	if failures > p.MaxRetries {
		return 0, false
	}
	if p.MaxTotalTime > 0 && elapsed >= p.MaxTotalTime {
		return 0, false
	}

	backoff = p.BaseBackoff
	if backoff <= 0 {
		backoff = defaultBaseBackoff
	}
	for i := 1; i < failures; i++ {
		backoff *= 2
		if p.MaxBackoff > 0 && backoff >= p.MaxBackoff {
			backoff = p.MaxBackoff
			break
		}
	}
	if p.MaxBackoff > 0 && backoff > p.MaxBackoff {
		backoff = p.MaxBackoff
	}
	// 剩余时间不够一次退避时按预算耗尽处理，避免退避跨越总耗时上限。
	if p.MaxTotalTime > 0 && elapsed+backoff > p.MaxTotalTime {
		return 0, false
	}
	return backoff, true
}

// Run 在重试预算内反复执行 process，直到底层给出确定结果。
//
// stop 用于查询当前是否已经开始停机，返回 true 时立即停止重试并返回 OutcomeStopped。
// now 与 sleep 允许注入，便于测试时间相关行为；传 nil 时使用 time.Now 与 time.Sleep。
func Run(
	policy Policy,
	process func() error,
	classify ClassifyFunc,
	stop func() bool,
	now func() time.Time,
	sleep func(time.Duration),
) (outcome Outcome, stage string, err error, attempts int) {
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	startedAt := now()
	for {
		attempts++
		err = process()
		if err == nil {
			return OutcomeSucceeded, "", nil, attempts
		}

		stage, retryable := "unknown", true
		if classify != nil {
			stage, retryable = classify(err)
		}
		if !retryable {
			return OutcomeNonRetryable, stage, err, attempts
		}

		backoff, allowed := policy.BackoffAfterFailures(attempts, now().Sub(startedAt))
		if !allowed {
			return OutcomeExhausted, stage, err, attempts
		}
		if stop != nil && stop() {
			return OutcomeStopped, stage, err, attempts
		}

		sleep(backoff)
	}
}
