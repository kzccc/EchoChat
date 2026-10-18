package chat

// 本文件把消费侧的"重试预算"接入到聊天消费链路：
// 1. 可重试失败不再无限原地重试，而是受次数上限、指数退避和总耗时上限三重约束；
// 2. 预算耗尽后把这条消息降级为不可重试，与不可重试失败同样处置：写死信 + 放行位点；
// 3. 只有服务停机这一种情况不产生"归宿"，位点保持不动，留给下次重放。
//
// 纯决策逻辑放在 internal/consumerretry 包中，便于脱离 MySQL/Redis/Kafka 单测覆盖。

import (
	"github.com/IBM/sarama"
	"go.uber.org/zap"

	"echo_chat_server/internal/config"
	"echo_chat_server/internal/consumerretry"
	"echo_chat_server/pkg/zlog"
)

// consumeRetryOutcome 描述一条消息在一次消费周期内的归宿。
type consumeRetryOutcome = consumerretry.Outcome

const (
	// consumeRetrySucceeded 表示本次处理成功。
	consumeRetrySucceeded = consumerretry.OutcomeSucceeded
	// consumeRetryNonRetryable 表示失败且显式标记为不可重试。
	consumeRetryNonRetryable = consumerretry.OutcomeNonRetryable
	// consumeRetryExhausted 表示可重试失败已经耗尽重试预算，按不可重试处置。
	consumeRetryExhausted = consumerretry.OutcomeExhausted
	// consumeRetryStopped 表示重试过程中服务开始停机，位点不能推进。
	consumeRetryStopped = consumerretry.OutcomeStopped
)

// resolveConsumeRetryPolicy 从配置读取当前进程的重试预算。
func resolveConsumeRetryPolicy() consumerretry.Policy {
	kafkaConfig := config.GetConfig().KafkaConfig
	return consumerretry.Policy{
		MaxRetries:   kafkaConfig.ResolveConsumerRetryMaxRetries(),
		BaseBackoff:  kafkaConfig.ResolveConsumerRetryBaseBackoff(),
		MaxBackoff:   kafkaConfig.ResolveConsumerRetryMaxBackoff(),
		MaxTotalTime: kafkaConfig.ResolveConsumerRetryMaxTotalDuration(),
	}
}

// consumeWithRetryBudget 在重试预算内反复执行 process，直到底层给出确定结果。
//
// process 每次调用都应当完整执行一遍这条消息的处理流程（失败时由它内部完成清理），
// stopSignal 用于查询当前是否已经开始停机。返回的 attempts 是本轮实际尝试次数。
func consumeWithRetryBudget(
	policy consumerretry.Policy,
	process func() error,
	stopSignal func() bool,
) (outcome consumeRetryOutcome, stage string, err error, attempts int) {
	return consumerretry.Run(policy, process, consumerErrorMeta, stopSignal, nil, nil)
}

// logConsumeRetryBudgetExhausted 记录一条消息因重试预算耗尽而被降级为死信。
//
// 这是一次"主动放弃"，必须留下结构化日志，否则线上没人会知道系统在把消息推进死信。
func logConsumeRetryBudgetExhausted(
	module string,
	message *sarama.ConsumerMessage,
	stage string,
	attempts int,
	err error,
) {
	fields := []zap.Field{
		zap.String("event", "kafka.consume.retry_budget_exhausted"),
		zap.String("module", module),
		zap.String("stage", stage),
		zap.Int("attempts", attempts),
	}
	if message != nil {
		fields = append(fields,
			zap.String("topic", message.Topic),
			zap.Int32("partition", message.Partition),
			zap.Int64("offset", message.Offset),
		)
	}
	if err != nil {
		fields = append(fields, zap.String("error", err.Error()))
	}
	zlog.Error("kafka.consume.retry_budget_exhausted", fields...)
}
