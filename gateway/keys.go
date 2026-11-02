package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GenerateRandomKey creates a new sk-omni- API key with 32 random bytes (64 hex chars).
func GenerateRandomKey() (fullKey string, maskedKey string, id string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	hexStr := hex.EncodeToString(b)
	fullKey = "sk-omni-" + hexStr
	maskedKey = "sk-omni-..." + hexStr[len(hexStr)-4:]

	idBytes := make([]byte, 6)
	_, _ = rand.Read(idBytes)
	id = "key-" + hex.EncodeToString(idBytes)
	return fullKey, maskedKey, id, nil
}

// ListAPIKeys returns all saved client API keys.
func ListAPIKeys() []GatewayAPIKey {
	cfg := LoadSavedAuthConfig()
	if cfg.APIKeys == nil {
		return []GatewayAPIKey{}
	}
	keys := make([]GatewayAPIKey, len(cfg.APIKeys))
	copy(keys, cfg.APIKeys)
	return keys
}

// CreateAPIKey generates, persists, and returns a new GatewayAPIKey.
func CreateAPIKey(name string, admin bool, selfUsage, sharedQuota, localUsageCmd bool) (GatewayAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default-key"
	}

	fullKey, maskedKey, id, err := GenerateRandomKey()
	if err != nil {
		return GatewayAPIKey{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	keyObj := GatewayAPIKey{
		ID:                 id,
		Name:               name,
		Key:                fullKey,
		MaskedKey:          maskedKey,
		AdminAccess:        admin,
		AllowSelfUsage:     selfUsage,
		AllowSharedQuota:   sharedQuota,
		AllowLocalUsageCmd: localUsageCmd,
		TotalRequests:      0,
		TotalTokens:        0,
		CreatedAt:          now,
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	cfg.APIKeys = append(cfg.APIKeys, keyObj)
	cfg.UpdatedAt = now
	if err := saveAuthConfigLocked(cfg); err != nil {
		return GatewayAPIKey{}, fmt.Errorf("failed to save api key: %w", err)
	}

	SyncAPIKeysToPostgresAsync()
	return keyObj, nil
}

// DeleteAPIKey removes an API key matching either its ID or full Key string.
func DeleteAPIKey(identifier string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return errors.New("missing key identifier")
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()

	var filtered []GatewayAPIKey
	var deletedID string
	found := false
	for _, k := range cfg.APIKeys {
		if k.ID == identifier || k.Key == identifier {
			found = true
			deletedID = k.ID
			continue
		}
		filtered = append(filtered, k)
	}

	if !found {
		return fmt.Errorf("key %q not found", identifier)
	}

	cfg.APIKeys = filtered
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveAuthConfigLocked(cfg); err != nil {
		return err
	}
	DeleteAPIKeyFromPostgresAsync(deletedID)
	return nil
}

// ValidateAPIKey checks if the incoming Authorization header contains a valid OmniRoute API key.
// If no API keys are configured in the gateway, it operates in Open Access mode and returns true.
func ValidateAPIKey(authHeader string) (bool, *GatewayAPIKey) {
	cfg := LoadSavedAuthConfig()

	// Open mode: no keys configured
	if len(cfg.APIKeys) == 0 {
		return true, nil
	}

	raw := strings.TrimSpace(authHeader)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "bearer ") {
		raw = strings.TrimSpace(raw[7:])
	} else if len(raw) >= 6 && strings.EqualFold(raw[:6], "bearer") {
		raw = strings.TrimSpace(raw[6:])
	}

	if raw == "" {
		return false, nil
	}

	for _, k := range cfg.APIKeys {
		if k.Key == raw {
			cp := k
			return true, &cp
		}
	}

	return false, nil
}

// RecordAPIKeyUsage increments the request and token usage statistics for an API key.
func RecordAPIKeyUsage(keyStr string, tokens int) {
	keyStr = strings.TrimSpace(keyStr)
	if keyStr == "" {
		return
	}
	if len(keyStr) >= 7 && strings.EqualFold(keyStr[:7], "bearer ") {
		keyStr = strings.TrimSpace(keyStr[7:])
	} else if len(keyStr) >= 6 && strings.EqualFold(keyStr[:6], "bearer") {
		keyStr = strings.TrimSpace(keyStr[6:])
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()

	now := time.Now().UTC().Format(time.RFC3339)
	updated := false
	for i := range cfg.APIKeys {
		if cfg.APIKeys[i].Key == keyStr || cfg.APIKeys[i].ID == keyStr {
			cfg.APIKeys[i].TotalRequests++
			if tokens > 0 {
				cfg.APIKeys[i].TotalTokens += int64(tokens)
			}
			cfg.APIKeys[i].LastUsedAt = now
			updated = true
			break
		}
	}

	if updated {
		cfg.UpdatedAt = now
		_ = saveAuthConfigLocked(cfg)
		SyncAPIKeysToPostgresAsync()
	}
}
