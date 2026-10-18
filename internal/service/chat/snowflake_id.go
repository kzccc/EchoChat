package chat

import (
	"fmt"
	"hash/fnv"
	"os"
	"sync"
	"time"

	"echo_chat_server/internal/config"
)

const (
	snowflakeEpochMillis = int64(1704067200000) // 2024-01-01 00:00:00 UTC
	snowflakeWorkerBits  = int64(10)
	snowflakeSeqBits     = int64(12)
	snowflakeMaxWorkerID = int64(1<<snowflakeWorkerBits) - 1
	snowflakeMaxSequence = int64(1<<snowflakeSeqBits) - 1
)

type snowflakeGenerator struct {
	mu       sync.Mutex
	workerID int64
	lastMs   int64
	sequence int64
}

var messageIDGenerator = newSnowflakeGenerator(resolveSnowflakeWorkerID())

func newSnowflakeGenerator(workerID int64) *snowflakeGenerator {
	if workerID < 0 || workerID > snowflakeMaxWorkerID {
		workerID = 0
	}
	return &snowflakeGenerator{workerID: workerID}
}

func resolveSnowflakeWorkerID() int64 {
	if configured := config.GetConfig().IDConfig.SnowflakeWorkerID; configured != nil {
		if *configured >= 0 && *configured <= snowflakeMaxWorkerID {
			return *configured
		}
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(hostname))
	return int64(hasher.Sum32()) & snowflakeMaxWorkerID
}

func (g *snowflakeGenerator) nextID() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()

	nowMs := time.Now().UnixMilli() - snowflakeEpochMillis
	if nowMs < g.lastMs {
		nowMs = g.lastMs
	}
	if nowMs == g.lastMs {
		g.sequence = (g.sequence + 1) & snowflakeMaxSequence
		if g.sequence == 0 {
			for nowMs <= g.lastMs {
				nowMs = time.Now().UnixMilli() - snowflakeEpochMillis
			}
		}
	} else {
		g.sequence = 0
	}
	g.lastMs = nowMs

	return (nowMs << (snowflakeWorkerBits + snowflakeSeqBits)) |
		(g.workerID << snowflakeSeqBits) |
		g.sequence
}

func buildSnowflakeMessageID() string {
	return fmt.Sprintf("M%d", messageIDGenerator.nextID())
}
