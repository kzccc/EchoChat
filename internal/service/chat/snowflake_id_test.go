package chat

import (
	"strings"
	"sync"
	"testing"
)

func TestBuildMessageIDSnowflakeUnique(t *testing.T) {
	const total = 1000
	ids := make(chan string, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids <- buildMessageID()
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]struct{}, total)
	for id := range ids {
		if !strings.HasPrefix(id, "M") {
			t.Fatalf("message id should keep M prefix, got %q", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate message id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != total {
		t.Fatalf("expected %d ids, got %d", total, len(seen))
	}
}
