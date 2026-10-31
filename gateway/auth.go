package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "omni_session"
	SessionDuration   = 7 * 24 * time.Hour
)

type sessionData struct {
	ExpiresAt time.Time
}

var (
	sessionsMu sync.RWMutex
	sessions   = make(map[string]sessionData)
)

// hashPassword returns a salted SHA-256 hash stretched 5,000 iterations.
func hashPassword(password, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	curr := append([]byte(password), salt...)
	for i := 0; i < 5000; i++ {
		h := sha256.Sum256(curr)
		curr = append(h[:], salt...)
	}
	finalHash := sha256.Sum256(curr)
	return hex.EncodeToString(finalHash[:])
}

func generateRandomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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

// VerifyAdminPassword checks whether the given password matches the stored admin hash.
func VerifyAdminPassword(password string) bool {
	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if strings.TrimSpace(cfg.AdminPasswordHash) == "" {
		return false
	}

	calculated := hashPassword(password, cfg.AdminSalt)
	return subtle.ConstantTimeCompare([]byte(calculated), []byte(cfg.AdminPasswordHash)) == 1
}

// ChangeAdminPassword updates the admin password after verifying the old password.
func ChangeAdminPassword(oldPassword, newPassword string) error {
	newPassword = strings.TrimSpace(newPassword)
	if len(newPassword) < 4 {
		return errors.New("new password must be at least 4 characters long")
	}

	configMu.Lock()
	defer configMu.Unlock()
	cfg := loadSavedAuthConfigLocked()
	if strings.TrimSpace(cfg.AdminPasswordHash) != "" {
		calculated := hashPassword(oldPassword, cfg.AdminSalt)
		if subtle.ConstantTimeCompare([]byte(calculated), []byte(cfg.AdminPasswordHash)) != 1 {
			return errors.New("incorrect old password")
		}
	}

	salt := generateRandomHex(16)
	cfg.AdminSalt = salt
	cfg.AdminPasswordHash = hashPassword(newPassword, salt)
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveAuthConfigLocked(cfg)
}

// CreateSession generates a new session token valid for 7 days.
func CreateSession() string {
	token := "omni-sess-" + generateRandomHex(32)
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions[token] = sessionData{
		ExpiresAt: time.Now().Add(SessionDuration),
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
	// 1. Check HttpOnly cookie
	if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 2. Check X-Admin-Token header
	if t := r.Header.Get("X-Admin-Token"); t != "" {
		return t
	}

	// 3. Check Authorization: Bearer <token>
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") || strings.HasPrefix(authHeader, "bearer ") {
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
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
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
	SetSessionCookie(w, sessionToken)

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

	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload: "+err.Error(), "invalid_request_error")
		return
	}

	if !VerifyAdminPassword(payload.Password) {
		writeOpenAIError(w, http.StatusUnauthorized, "mật khẩu không chính xác", "auth_failed")
		return
	}

	sessionToken := CreateSession()
	SetSessionCookie(w, sessionToken)

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

func (s *Server) handleAuthPasswordChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "only POST is supported", "invalid_request_error")
		return
	}

	authed, _ := IsAdminRequestAuthenticated(r)
	if !authed {
		writeOpenAIError(w, http.StatusUnauthorized, "bạn cần đăng nhập trước khi đổi mật khẩu", "unauthorized")
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

	if err := ChangeAdminPassword(payload.OldPassword, payload.NewPassword); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error(), "password_change_error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Đổi mật khẩu thành công",
	})
}

