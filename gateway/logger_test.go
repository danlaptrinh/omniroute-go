package gateway

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLoggerRingBufferAndFiltering(t *testing.T) {
	logger := NewGatewayLogger(5)

	// 1. Initial state
	if len(logger.GetRecentLogs(10, "", false)) != 0 {
		t.Fatalf("expected empty logs initially")
	}

	// 2. Add 7 entries (capacity is 5, so first 2 should drop)
	for i := 1; i <= 7; i++ {
		isErr := (i%2 == 0)
		status := 200
		errMsg := ""
		if isErr {
			status = 502
			errMsg = fmt.Sprintf("Upstream failure %d", i)
		}
		prov := ProviderAntigravity
		if i%3 == 0 {
			prov = ProviderClaudeCode
		} else if i%3 == 1 {
			prov = ProviderAlibaba
		}

		logger.RecordLogEntry(GatewayLogEntry{
			Provider:       prov,
			RequestedModel: fmt.Sprintf("model-%d", i),
			ResolvedModel:  fmt.Sprintf("resolved-%d", i),
			StatusCode:     status,
			LatencyMs:      int64(i * 10),
			IsError:        isErr,
			ErrorMessage:   errMsg,
		})
	}

	// 3. Ring buffer capping: only entries 3..7 (total 5) should remain
	all := logger.GetRecentLogs(10, "", false)
	if len(all) != 5 {
		t.Fatalf("expected 5 logs due to capacity capping, got %d", len(all))
	}
	// Most recent should be entry 7 (first item in result)
	if all[0].RequestedModel != "model-7" {
		t.Errorf("expected newest entry model-7 first, got %s", all[0].RequestedModel)
	}
	if all[len(all)-1].RequestedModel != "model-3" {
		t.Errorf("expected oldest remaining entry model-3 last, got %s", all[len(all)-1].RequestedModel)
	}

	// 4. Test error filtering
	errsOnly := logger.GetRecentLogs(10, "", true)
	if len(errsOnly) == 0 {
		t.Errorf("expected error logs to be found")
	}
	for _, e := range errsOnly {
		if !e.IsError && e.StatusCode < 400 && e.ErrorMessage == "" {
			t.Errorf("unexpected non-error entry in errorsOnly filter: %+v", e)
		}
	}

	// 5. Test provider filtering
	claudeLogs := logger.GetRecentLogs(10, "claude-code", false)
	for _, e := range claudeLogs {
		if e.Provider != ProviderClaudeCode {
			t.Errorf("expected only claude-code logs, got %s", e.Provider)
		}
	}

	// 6. Test clear logs
	logger.ClearLogs()
	if len(logger.GetRecentLogs(10, "", false)) != 0 {
		t.Errorf("expected 0 logs after ClearLogs()")
	}
}

func TestLoggerCappingAndAutoFields(t *testing.T) {
	logger := NewGatewayLogger(3)

	// Record with empty ID and timestamp: verify defaults are populated
	logger.RecordLogEntry(GatewayLogEntry{
		Provider:       ProviderAntigravity,
		RequestedModel: "test-model",
		StatusCode:     200,
	})

	recent := logger.GetRecentLogs(1, "", false)
	if len(recent) != 1 {
		t.Fatalf("expected 1 log, got %d", len(recent))
	}
	if recent[0].ID == "" {
		t.Errorf("expected auto-generated log ID")
	}
	if recent[0].Timestamp == "" {
		t.Errorf("expected auto-generated timestamp")
	}

	// Test status code >= 400 sets IsError to true
	logger.RecordLogEntry(GatewayLogEntry{
		Provider:   ProviderAlibaba,
		StatusCode: 500,
	})
	recentErrors := logger.GetRecentLogs(1, "", true)
	if len(recentErrors) != 1 || !recentErrors[0].IsError {
		t.Errorf("expected StatusCode >= 400 to mark IsError=true")
	}

	// Add more entries to test exact 3-item limit
	logger.RecordLogEntry(GatewayLogEntry{Provider: ProviderClaudeCode, StatusCode: 200, RequestedModel: "claude-1"})
	logger.RecordLogEntry(GatewayLogEntry{Provider: ProviderClaudeCode, StatusCode: 200, RequestedModel: "claude-2"})

	if logger.Len() != 3 {
		t.Errorf("expected buffer len to be 3, got %d", logger.Len())
	}

	logsAll := logger.GetRecentLogs(0, "", false) // limit=0 returns all
	if len(logsAll) != 3 {
		t.Errorf("expected 3 logs when limit=0, got %d", len(logsAll))
	}
}

func TestLoggerGlobalSingleton(t *testing.T) {
	ClearLogs()
	if len(GetRecentLogs(10, "", false)) != 0 {
		t.Fatalf("expected GlobalLogger to be empty after ClearLogs()")
	}

	RecordLogEntry(GatewayLogEntry{
		Provider:       ProviderAntigravity,
		RequestedModel: "global-model-1",
		StatusCode:     200,
	})
	RecordLogEntry(GatewayLogEntry{
		Provider:       ProviderClaudeCode,
		RequestedModel: "global-model-2",
		StatusCode:     401,
		ErrorMessage:   "Unauthorized",
	})

	all := GetRecentLogs(10, "", false)
	if len(all) != 2 {
		t.Fatalf("expected 2 global logs, got %d", len(all))
	}

	errs := GetRecentLogs(10, "", true)
	if len(errs) != 1 || errs[0].RequestedModel != "global-model-2" {
		t.Errorf("expected 1 global error log for model-2, got %+v", errs)
	}

	claudeOnly := GetRecentLogs(10, "claude-code", false)
	if len(claudeOnly) != 1 || claudeOnly[0].Provider != ProviderClaudeCode {
		t.Errorf("expected 1 claude log, got %+v", claudeOnly)
	}

	ClearLogs()
	if len(GetRecentLogs(10, "", false)) != 0 {
		t.Errorf("expected 0 logs after ClearLogs()")
	}
}

func TestLoggerConcurrency(t *testing.T) {
	logger := NewGatewayLogger(50)
	var wg sync.WaitGroup
	const workers = 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				logger.RecordLogEntry(GatewayLogEntry{
					Provider:       ProviderAntigravity,
					RequestedModel: fmt.Sprintf("worker-%d-req-%d", workerID, j),
					StatusCode:     200 + (j % 3 * 100),
					LatencyMs:      int64(j * 5),
				})
				_ = logger.GetRecentLogs(5, "antigravity", false)
				_ = logger.GetRecentLogs(5, "", true)
				time.Sleep(1 * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	if logger.Len() > 50 {
		t.Errorf("expected logger ring buffer capped at 50, got %d", logger.Len())
	}
	logs := logger.GetRecentLogs(100, "", false)
	if len(logs) > 50 {
		t.Errorf("expected max 50 logs returned, got %d", len(logs))
	}
}
