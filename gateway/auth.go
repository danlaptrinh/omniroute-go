package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "omni_session"
	SessionDuration   = 7 * 24 * time.Hour
	MaxActiveSessions = 256

	passwordHashV2Prefix     = "v2$"
	passwordHashV2Iterations = 100000
	legacyHashIterations     = 5000

	loginRateWindow      = 5 * time.Minute
	loginMaxFailures     = 8
	loginLockoutDuration = 60 * time.Second
)

type sessionData struct {
	ExpiresAt time.Time
}

type loginAttemptEntry struct {
	failures    []time.Time
	lockedUntil time.Time
	lastSeen    time.Time
}

var (
	sessionsMu sync.RWMutex
	sessions   = make(map[string]sessionData)

	loginRateMu   sync.Mutex
	loginAttempts = make(map[string]*loginAttemptEntry)
)

// legacyHashPassword returns the legacy salted SHA-256 hash stretched 5,000 iterations (without v2$ prefix).
func legacyHashPassword(password, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	curr := append([]byte(password), salt...)
	buf := make([]byte, 32+len(salt))
	copy(buf[32:], salt)
	for i := 0; i < legacyHashIterations; i++ {
		h := sha256.Sum256(curr)
		copy(buf[:32], h[:])
		curr = buf
	}
	finalHash := sha256.Sum256(curr)
	return hex.EncodeToString(finalHash[:])
}

// hashPassword returns a salted SHA-256 hash stretched 100,000 iterations prefixed with "v2$".
func hashPassword(password, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	curr := append([]byte(password), salt...)
	buf := make([]byte, 32+len(salt))
	copy(buf[32:], salt)
	for i := 0; i < passwordHashV2Iterations; i++ {
		h := sha256.Sum256(curr)
		copy(buf[:32], h[:])
		curr = buf
	}
	finalHash := sha256.Sum256(curr)
	return passwordHashV2Prefix + hex.EncodeToString(finalHash[:])
}

func verifyStoredPasswordHash(password, saltHex, storedHash string) bool {
	storedHash = strings.TrimSpace(storedHash)
	if storedHash == "" {
		return false
	}
	if strings.HasPrefix(storedHash, passwordHashV2Prefix) {
		calculated := hashPassword(password, saltHex)
		return subtle.ConstantTimeCompare([]byte(calculated), []byte(storedHash)) == 1
	}
	calculatedLegacy := legacyHashPassword(password, saltHex)
	return subtle.ConstantTimeCompare([]byte(calculatedLegacy), []byte(storedHash)) == 1
}

func generateRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func normalizeLoginIPKey(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(ip); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return ip
}

func clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	remote := normalizeLoginIPKey(r.RemoteAddr)
	isLocalProxy := remote == "" || remote == "unknown" || remote == "127.0.0.1" || remote == "::1" || remote == "localhost"
	if isLocalProxy {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if first := normalizeLoginIPKey(parts[0]); first != "" && first != "unknown" {
				return first
			}
		}
		if xri := normalizeLoginIPKey(r.Header.Get("X-Real-IP")); xri != "" && xri != "unknown" {
			return xri
		}
	}
	return remote
}

func sweepStaleLoginAttemptsLocked(now time.Time) {
	for k, entry := range loginAttempts {
		if entry == nil {
			delete(loginAttempts, k)
			continue
		}
		if !now.Before(entry.lockedUntil) && now.Sub(entry.lastSeen) > loginRateWindow {
			delete(loginAttempts, k)
		}
	}
}

// CheckLoginRateLimit checks whether the given client IP is allowed to attempt authentication.
// If locked out, returns (false, retryAfterSeconds).
func CheckLoginRateLimit(ip string) (allowed bool, retryAfterSec int) {
	key := normalizeLoginIPKey(ip)
	now := time.Now()

	loginRateMu.Lock()
	defer loginRateMu.Unlock()

	sweepStaleLoginAttemptsLocked(now)
	entry, exists := loginAttempts[key]
	if !exists || entry == nil {
		return true, 0
	}

	if now.Before(entry.lockedUntil) {
		rem := int(math.Ceil(entry.lockedUntil.Sub(now).Seconds()))
		if rem < 1 {
			rem = 1
		}
		return false, rem
	}

	return true, 0
}

// RecordLoginFailure records a failed password attempt for the given IP and locks it out for 60s after 8 failures within 5m.
func RecordLoginFailure(ip string) {
	key := normalizeLoginIPKey(ip)
	now := time.Now()

	loginRateMu.Lock()
	defer loginRateMu.Unlock()

	sweepStaleLoginAttemptsLocked(now)
	entry, exists := loginAttempts[key]
	if !exists || entry == nil {
		entry = &loginAttemptEntry{}
		loginAttempts[key] = entry
	}

	kept := entry.failures[:0]
	for _, ts := range entry.failures {
		if now.Sub(ts) <= loginRateWindow {
			kept = append(kept, ts)
		}
	}
	entry.failures = append(kept, now)
	entry.lastSeen = now

	if len(entry.failures) >= loginMaxFailures {
		entry.lockedUntil = now.Add(loginLockoutDuration)
	}
}

// ResetLoginFailures clears all recorded login failures and lockouts for the given IP.
func ResetLoginFailures(ip string) {
	key := normalizeLoginIPKey(ip)
	loginRateMu.Lock()
	defer loginRateMu.Unlock()
	delete(loginAttempts, key)
}

// IsAdminConfigured returns true if an administrator password has been set.
func IsAdminConfigured() bool {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	return strings.TrimSpace(cfg.AdminPasswordHash) != ""
}

// SetupAdminPassword configures the admin password if not previously set.
func SetupAdminPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 4 {
		return errors.New("password must be at least 4 characters long")
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if strings.TrimSpace(cfg.AdminPasswordHash) != "" {
		return errors.New("admin password already configured; use change-password instead")
	}

	salt := generateRandomHex(16)
	cfg.AdminSalt = salt
	cfg.AdminPasswordHash = hashPassword(password, salt)
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// VerifyAdminPassword checks whether the given password matches the stored admin hash (supporting both v2$ and legacy hashes).
func VerifyAdminPassword(password string) bool {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	return verifyStoredPasswordHash(password, cfg.AdminSalt, cfg.AdminPasswordHash)
}

// ChangeAdminPassword updates the admin password after verifying the old password, and revokes all active sessions.
func ChangeAdminPassword(oldPassword, newPassword string) error {
	newPassword = strings.TrimSpace(newPassword)
	if len(newPassword) < 4 {
		return errors.New("new password must be at least 4 characters long")
	}

	configMu.Lock()
	cfg := loadSavedAuthConfigLocked()
	if strings.TrimSpace(cfg.AdminPasswordHash) != "" {
		if !verifyStoredPasswordHash(oldPassword, cfg.AdminSalt, cfg.AdminPasswordHash) {
			configMu.Unlock()
			return errors.New("incorrect old password")
		}
	}

	salt := generateRandomHex(16)
	cfg.AdminSalt = salt
	cfg.AdminPasswordHash = hashPassword(newPassword, salt)
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	err := saveAuthConfigLocked(cfg)
	configMu.Unlock()
	if err != nil {
		return err
	}

	sessionsMu.Lock()
	sessions = make(map[string]sessionData)
	sessionsMu.Unlock()
	return nil
}

// CreateSession generates a new session token valid for 7 days, sweeping expired sessions and capping active sessions at 256.
func CreateSession() string {
	token := "omni-sess-" + generateRandomHex(32)
	now := time.Now()

	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	for k, v := range sessions {
		if now.After(v.ExpiresAt) {
			delete(sessions, k)
		}
	}

	for len(sessions) >= MaxActiveSessions {
		var oldestKey string
		var oldestExp time.Time
		first := true
		for k, v := range sessions {
			if first || v.ExpiresAt.Before(oldestExp) {
				oldestKey = k
				oldestExp = v.ExpiresAt
				first = false
			}
		}
		if oldestKey == "" {
			break
		}
		delete(sessions, oldestKey)
	}

	sessions[token] = sessionData{
		ExpiresAt: now.Add(SessionDuration),
	}
	return token
}

// ValidateSession verifies if a session token is active and unexpired.
func ValidateSession(token string) bool {
	if token == "" {
		return false
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	data, exists := sessions[token]
	if !exists {
		return false
	}
	if time.Now().After(data.ExpiresAt) {
		delete(sessions, token)
		return false
	}
	return true
}

// RevokeSession deletes an active session.
func RevokeSession(token string) {
	if token == "" {
		return
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	delete(sessions, token)
}

// GetTokenFromRequest extracts the token from cookies or Authorization header.
func GetTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	// 1. Check HttpOnly cookie
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 2. Check X-Admin-Token header
	if t := strings.TrimSpace(r.Header.Get("X-Admin-Token")); t != "" {
		return t
	}

	// 3. Check Authorization: Bearer <token>
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(authHeader) >= 7 && strings.EqualFold(authHeader[:7], "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

// IsAdminRequestAuthenticated checks if request carries a valid admin session or admin API key.
// Returns (isAuthenticated, needsSetup).
func IsAdminRequestAuthenticated(r *http.Request) (bool, bool) {
	configured := IsAdminConfigured()
	if !configured {
		// If admin password is not yet configured, flag needsSetup=true
		return false, true
	}

	token := GetTokenFromRequest(r)
	if token == "" {
		return false, false
	}

	// 1. Check if token is an active session
	if ValidateSession(token) {
		return true, false
	}

	// 2. Check if token is an API key with AdminAccess
	valid, keyObj := ValidateAPIKey("Bearer " + token)
	if valid && keyObj != nil && keyObj.AdminAccess {
		return true, false
	}

	return false, false
}

// SetSessionCookie writes the HTTP session cookie onto response.
func SetSessionCookie(w http.ResponseWriter, token string) {
	SetSessionCookieForRequest(w, nil, token)
}

// SetSessionCookieForRequest writes the HTTP session cookie onto response, setting Secure automatically over HTTPS.
func SetSessionCookieForRequest(w http.ResponseWriter, r *http.Request, token string) {
	secure := r != nil && (r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https"))
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionDuration.Seconds()),
	})
}

// ClearSessionCookie removes the HTTP session cookie from client.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// --- HTTP API Handlers for Authentication ---

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	configured := IsAdminConfigured()
	authed, needsSetup := IsAdminRequestAuthenticated(r)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"configured":    configured,
		"authenticated": authed,
		"needs_setup":   needsSetup,
		"user":          "admin",
	})
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if err := SetupAdminPassword(payload.Password); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "setup_error")
		return
	}

	sessionToken := CreateSession()
	SetSessionCookieForRequest(w, r, sessionToken)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Admin password configured successfully",
		"token":   sessionToken,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	ip := clientIP(r)
	if allowed, retryAfter := CheckLoginRateLimit(ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeOpenAIError(w, http.StatusTooManyRequests, fmt.Sprintf("quá nhiều lần thử đăng nhập sai; vui lòng thử lại sau %d giây", retryAfter), "rate_limit_exceeded")
		return
	}

	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if !VerifyAdminPassword(payload.Password) {
		RecordLoginFailure(ip)
		writeOpenAIError(w, http.StatusUnauthorized, "mật khẩu không chính xác", "auth_failed")
		return
	}

	ResetLoginFailures(ip)
	sessionToken := CreateSession()
	SetSessionCookieForRequest(w, r, sessionToken)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Đăng nhập thành công",
		"token":   sessionToken,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	RevokeSession(token)
	ClearSessionCookie(w)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Đã đăng xuất",
	})
}

func (s *Server) handleAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	s.handleAuthPasswordChange(w, r)
}

func (s *Server) handleAuthPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	ip := clientIP(r)
	if allowed, retryAfter := CheckLoginRateLimit(ip); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeOpenAIError(w, http.StatusTooManyRequests, fmt.Sprintf("quá nhiều lần thử mật khẩu sai; vui lòng thử lại sau %d giây", retryAfter), "rate_limit_exceeded")
		return
	}

	var payload struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	authed, _ := IsAdminRequestAuthenticated(r)
	configured := IsAdminConfigured()
	if !configured {
		writeOpenAIError(w, http.StatusUnauthorized, "chưa thiết lập mật khẩu quản trị; hãy dùng /v1/auth/setup", "unauthorized")
		return
	}
	if !authed && strings.TrimSpace(payload.OldPassword) == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "bạn cần đăng nhập trước khi đổi mật khẩu", "unauthorized")
		return
	}
	if !VerifyAdminPassword(payload.OldPassword) {
		RecordLoginFailure(ip)
		writeOpenAIError(w, http.StatusUnauthorized, "mật khẩu cũ không chính xác", "password_change_error")
		return
	}

	if err := ChangeAdminPassword(payload.OldPassword, payload.NewPassword); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "password_change_error")
		return
	}

	ResetLoginFailures(ip)
	freshToken := CreateSession()
	SetSessionCookieForRequest(w, r, freshToken)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Đổi mật khẩu thành công",
		"token":   freshToken,
	})
}
