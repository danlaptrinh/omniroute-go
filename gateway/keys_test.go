package gateway

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestKeyGenerationFormat(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	fullKey, maskedKey, id, err := GenerateRandomKey()
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}

	// Must start with sk-omni-
	if !strings.HasPrefix(fullKey, "sk-omni-") {
		t.Errorf("expected key to start with 'sk-omni-', got %s", fullKey)
	}

	// Prefix is 8 chars ("sk-omni-"), 32 hex bytes = 64 hex characters, total 72 chars
	hexPart := strings.TrimPrefix(fullKey, "sk-omni-")
	if len(hexPart) != 64 {
		t.Errorf("expected 64 hex chars (32 bytes), got length %d (%s)", len(hexPart), hexPart)
	}

	// Masked key format: sk-omni-...<last 4 hex>
	expectedMasked := "sk-omni-..." + hexPart[len(hexPart)-4:]
	if maskedKey != expectedMasked {
		t.Errorf("expected masked key %s, got %s", expectedMasked, maskedKey)
	}

	// ID must start with key-
	if !strings.HasPrefix(id, "key-") {
		t.Errorf("expected id to start with 'key-', got %s", id)
	}
}

func TestKeyLifecycleAndValidation(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// 1. Initial state: Open mode (no keys configured)
	valid, matched := ValidateAPIKey("")
	if !valid || matched != nil {
		t.Fatalf("expected open mode to allow empty key, got valid=%v", valid)
	}

	validAny, matchedAny := ValidateAPIKey("Bearer any-random-token")
	if !validAny || matchedAny != nil {
		t.Fatalf("expected open mode to allow any token, got valid=%v", validAny)
	}

	// 2. Create an admin key
	key1, err := CreateAPIKey("Production Key", true, true, false, false)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	if !strings.HasPrefix(key1.Key, "sk-omni-") {
		t.Errorf("expected sk-omni- prefix, got %s", key1.Key)
	}
	if !strings.HasPrefix(key1.MaskedKey, "sk-omni-...") {
		t.Errorf("expected sk-omni-... masked format, got %s", key1.MaskedKey)
	}
	if !key1.AdminAccess || !key1.AllowSelfUsage {
		t.Errorf("unexpected permissions: %+v", key1)
	}
	if key1.CreatedAt == "" {
		t.Errorf("expected CreatedAt timestamp to be set")
	}

	// 3. Create a second key with default name when empty
	key2, err := CreateAPIKey("", false, false, true, true)
	if err != nil {
		t.Fatalf("CreateAPIKey 2 failed: %v", err)
	}
	if key2.Name != "default-key" {
		t.Errorf("expected default name 'default-key', got %s", key2.Name)
	}

	// 4. List keys
	allKeys := ListAPIKeys()
	if len(allKeys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(allKeys))
	}

	// 5. Validation now requires a key
	validEmpty, _ := ValidateAPIKey("")
	if validEmpty {
		t.Errorf("expected empty key to be rejected when keys exist")
	}

	validWrong, _ := ValidateAPIKey("Bearer sk-invalid-key")
	if validWrong {
		t.Errorf("expected invalid key to be rejected")
	}

	// Validate with "Bearer " prefix
	validKey1, matched1 := ValidateAPIKey("Bearer " + key1.Key)
	if !validKey1 || matched1 == nil || matched1.ID != key1.ID {
		t.Errorf("expected key1 to validate successfully with 'Bearer ', got valid=%v matched=%+v", validKey1, matched1)
	}

	// Validate with lowercase "bearer " prefix
	validKey1Lower, matched1Lower := ValidateAPIKey("bearer " + key1.Key)
	if !validKey1Lower || matched1Lower == nil || matched1Lower.ID != key1.ID {
		t.Errorf("expected key1 to validate successfully with 'bearer ', got valid=%v matched=%+v", validKey1Lower, matched1Lower)
	}

	// Validate with raw key (without Bearer prefix)
	validKey2Raw, matched2Raw := ValidateAPIKey(key2.Key)
	if !validKey2Raw || matched2Raw == nil || matched2Raw.ID != key2.ID {
		t.Errorf("expected key2 to validate successfully with raw token, got valid=%v matched=%+v", validKey2Raw, matched2Raw)
	}

	// 6. Record usage
	RecordAPIKeyUsage(key1.Key, 250)
	afterUsage := ListAPIKeys()
	var foundKey1 *GatewayAPIKey
	for _, k := range afterUsage {
		if k.ID == key1.ID {
			foundKey1 = &k
			break
		}
	}
	if foundKey1 == nil || foundKey1.TotalRequests != 1 || foundKey1.TotalTokens != 250 || foundKey1.LastUsedAt == "" {
		t.Errorf("expected usage recorded (requests=1, tokens=250), got %+v", foundKey1)
	}

	// Record usage again with "Bearer " prefix and zero tokens
	RecordAPIKeyUsage("Bearer "+key1.Key, 0)
	afterUsage2 := ListAPIKeys()
	for _, k := range afterUsage2 {
		if k.ID == key1.ID {
			if k.TotalRequests != 2 || k.TotalTokens != 250 {
				t.Errorf("expected requests=2 and tokens=250, got requests=%d tokens=%d", k.TotalRequests, k.TotalTokens)
			}
			break
		}
	}

	// Record usage by Key ID
	RecordAPIKeyUsage(key2.ID, 100)
	afterUsage3 := ListAPIKeys()
	for _, k := range afterUsage3 {
		if k.ID == key2.ID {
			if k.TotalRequests != 1 || k.TotalTokens != 100 {
				t.Errorf("expected key2 requests=1 tokens=100, got requests=%d tokens=%d", k.TotalRequests, k.TotalTokens)
			}
			break
		}
	}

	// 7. Delete key by ID
	if err := DeleteAPIKey(key2.ID); err != nil {
		t.Fatalf("DeleteAPIKey failed: %v", err)
	}
	remaining := ListAPIKeys()
	if len(remaining) != 1 || remaining[0].ID != key1.ID {
		t.Errorf("expected 1 remaining key, got %d", len(remaining))
	}

	// Delete key by Key string
	if err := DeleteAPIKey(key1.Key); err != nil {
		t.Fatalf("DeleteAPIKey by Key string failed: %v", err)
	}
	remainingAfterAll := ListAPIKeys()
	if len(remainingAfterAll) != 0 {
		t.Errorf("expected 0 remaining keys, got %d", len(remainingAfterAll))
	}

	// Back to open mode after deleting all keys
	validOpenAgain, _ := ValidateAPIKey("")
	if !validOpenAgain {
		t.Errorf("expected gateway to return to open mode when all keys deleted")
	}

	// Delete non-existent key returns error
	if err := DeleteAPIKey("non-existent-key"); err == nil {
		t.Errorf("expected error deleting non-existent key, got nil")
	}

	// Delete empty identifier returns error
	if err := DeleteAPIKey(""); err == nil {
		t.Errorf("expected error deleting empty identifier, got nil")
	}
}

func TestKeyConcurrency(t *testing.T) {
	tmpCfg := filepath.Join(t.TempDir(), "gateway_config.json")
	t.Setenv("GATEWAY_CONFIG_FILE", tmpCfg)

	// Create 10 keys concurrently
	const count = 10
	var wg sync.WaitGroup
	createdKeys := make([]GatewayAPIKey, count)
	errs := make([]error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			k, err := CreateAPIKey(fmt.Sprintf("worker-%d", idx), false, true, true, false)
			createdKeys[idx] = k
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for idx, err := range errs {
		if err != nil {
			t.Fatalf("concurrent CreateAPIKey [%d] failed: %v", idx, err)
		}
	}

	all := ListAPIKeys()
	if len(all) != count {
		t.Fatalf("expected %d keys, got %d", count, len(all))
	}

	// Record usage concurrently across all keys
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			RecordAPIKeyUsage(createdKeys[idx].Key, 50)
			_, _ = ValidateAPIKey("Bearer " + createdKeys[idx].Key)
		}(i)
	}
	wg.Wait()

	// Verify all had requests incremented
	finalKeys := ListAPIKeys()
	for _, k := range finalKeys {
		if k.TotalRequests < 1 || k.TotalTokens < 50 {
			t.Errorf("key %s did not record expected usage: reqs=%d tokens=%d", k.ID, k.TotalRequests, k.TotalTokens)
		}
	}
}
