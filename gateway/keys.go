package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ComputeAPIKeyHash returns the lowercase hex SHA-256 digest of the trimmed rawKey.
func ComputeAPIKeyHash(rawKey string) string {
	trimmed := strings.TrimSpace(rawKey)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

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

// ListAPIKeys returns all saved client API keys (including full keys for internal persistence and sync).
func ListAPIKeys() []GatewayAPIKey {
	cfg := LoadSavedAuthConfig()
	if cfg.APIKeys == nil {
		return []GatewayAPIKey{}
	}
	keys := make([]GatewayAPIKey, len(cfg.APIKeys))
	copy(keys, cfg.APIKeys)
	for i := range keys {
		if keys[i].KeyHash == "" && strings.TrimSpace(keys[i].Key) != "" {
			keys[i].KeyHash = ComputeAPIKeyHash(keys[i].Key)
		}
	}
	return keys
}

// ListSanitizedAPIKeys returns a copy of ListAPIKeys() with every item's raw Key cleared for safe HTTP responses.
func ListSanitizedAPIKeys() []GatewayAPIKey {
	keys := ListAPIKeys()
	for i := range keys {
		keys[i].Key = ""
	}
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
		KeyHash:            ComputeAPIKeyHash(fullKey),
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
		keyMatch := k.Key != "" && subtle.ConstantTimeCompare([]byte(k.Key), []byte(identifier)) == 1
		if k.ID == identifier || keyMatch {
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

	token := strings.TrimSpace(authHeader)
	if len(token) >= 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	} else if len(token) >= 6 && strings.EqualFold(token[:6], "bearer") {
		token = strings.TrimSpace(token[6:])
	}

	if token == "" {
		return false, nil
	}

	tokenBytes := []byte(token)
	var tokenHash string
	for _, k := range cfg.APIKeys {
		if k.Key != "" {
			if subtle.ConstantTimeCompare([]byte(k.Key), tokenBytes) == 1 {
				cp := k
				if cp.KeyHash == "" {
					cp.KeyHash = ComputeAPIKeyHash(cp.Key)
				}
				return true, &cp
			}
		} else if k.KeyHash != "" {
			if tokenHash == "" {
				tokenHash = ComputeAPIKeyHash(token)
			}
			if subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(tokenHash)) == 1 {
				cp := k
				return true, &cp
			}
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
	keyBytes := []byte(keyStr)
	for i := range cfg.APIKeys {
		keyMatch := cfg.APIKeys[i].Key != "" && subtle.ConstantTimeCompare([]byte(cfg.APIKeys[i].Key), keyBytes) == 1
		if keyMatch || cfg.APIKeys[i].ID == keyStr {
			cfg.APIKeys[i].TotalRequests++
			if tokens > 0 {
				cfg.APIKeys[i].TotalTokens += int64(tokens)
			}
			cfg.APIKeys[i].LastUsedAt = now
			if cfg.APIKeys[i].KeyHash == "" && cfg.APIKeys[i].Key != "" {
				cfg.APIKeys[i].KeyHash = ComputeAPIKeyHash(cfg.APIKeys[i].Key)
			}
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
