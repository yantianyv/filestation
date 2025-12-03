package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

type AuthManager struct {
	mu              sync.RWMutex
	passwordHash    string
	sessions        map[string]time.Time
	csrfTokens      map[string]time.Time
	loginAttempts   map[string][]time.Time // IP -> attempts
	lastCleanup     time.Time
}

const (
	sessionDuration    = 24 * time.Hour
	csrfTokenDuration  = 2 * time.Hour
	maxLoginAttempts   = 5
	loginWindow        = 15 * time.Minute
	cleanupInterval    = 1 * time.Hour
	minPasswordLength  = 8
)

func New() *AuthManager {
	am := &AuthManager{
		passwordHash:  "",
		sessions:      make(map[string]time.Time),
		csrfTokens:    make(map[string]time.Time),
		loginAttempts: make(map[string][]time.Time),
		lastCleanup:   time.Now(),
	}
	// Default password: admin123
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	am.passwordHash = string(hash)
	
	// Start cleanup goroutine
	go am.cleanupRoutine()
	
	return am
}

func (am *AuthManager) Login(password string, ip string) (string, bool) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Check rate limiting
	if am.isRateLimited(ip) {
		return "", false
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(am.passwordHash), []byte(password)); err != nil {
		am.recordFailedAttempt(ip)
		return "", false
	}

	// Clear failed attempts on success
	delete(am.loginAttempts, ip)

	// Create session
	sessionToken := generateToken()
	am.sessions[sessionToken] = time.Now().Add(sessionDuration)
	return sessionToken, true
}

func (am *AuthManager) VerifySession(token string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	expiry, ok := am.sessions[token]
	if !ok {
		return false
	}
	return time.Now().Before(expiry)
}

func (am *AuthManager) Logout(token string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	delete(am.sessions, token)
}

func (am *AuthManager) ChangePassword(oldPassword, newPassword string) (bool, string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(am.passwordHash), []byte(oldPassword)); err != nil {
		return false, "原密码错误"
	}

	// Validate new password
	if err := am.validatePassword(newPassword); err != nil {
		return false, err.Error()
	}

	// Update password
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return false, "密码加密失败"
	}
	am.passwordHash = string(hash)
	
	// Invalidate all sessions (force re-login)
	am.sessions = make(map[string]time.Time)
	
	return true, ""
}

func (am *AuthManager) validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return &PasswordError{Message: "密码长度至少8位"}
	}

	var hasUpper, hasLower, hasNumber, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper || !hasLower || !hasNumber {
		return &PasswordError{Message: "密码必须包含大小写字母和数字"}
	}

	return nil
}

func (am *AuthManager) GenerateCSRFToken() string {
	am.mu.Lock()
	defer am.mu.Unlock()

	token := generateToken()
	am.csrfTokens[token] = time.Now().Add(csrfTokenDuration)
	return token
}

func (am *AuthManager) VerifyCSRFToken(token string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()

	expiry, ok := am.csrfTokens[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(am.csrfTokens, token)
		return false
	}
	// Delete token after use (one-time use)
	delete(am.csrfTokens, token)
	return true
}

func (am *AuthManager) isRateLimited(ip string) bool {
	attempts, ok := am.loginAttempts[ip]
	if !ok {
		return false
	}

	// Remove old attempts
	now := time.Now()
	validAttempts := []time.Time{}
	for _, attempt := range attempts {
		if now.Sub(attempt) < loginWindow {
			validAttempts = append(validAttempts, attempt)
		}
	}
	am.loginAttempts[ip] = validAttempts

	return len(validAttempts) >= maxLoginAttempts
}

func (am *AuthManager) recordFailedAttempt(ip string) {
	attempts := am.loginAttempts[ip]
	attempts = append(attempts, time.Now())
	am.loginAttempts[ip] = attempts
}

func (am *AuthManager) cleanupRoutine() {
	ticker := time.NewTicker(cleanupInterval)
	for range ticker.C {
		am.cleanup()
	}
}

func (am *AuthManager) cleanup() {
	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()

	// Clean expired sessions
	for token, expiry := range am.sessions {
		if now.After(expiry) {
			delete(am.sessions, token)
		}
	}

	// Clean expired CSRF tokens
	for token, expiry := range am.csrfTokens {
		if now.After(expiry) {
			delete(am.csrfTokens, token)
		}
	}

	// Clean old login attempts
	for ip, attempts := range am.loginAttempts {
		validAttempts := []time.Time{}
		for _, attempt := range attempts {
			if now.Sub(attempt) < loginWindow {
				validAttempts = append(validAttempts, attempt)
			}
		}
		if len(validAttempts) == 0 {
			delete(am.loginAttempts, ip)
		} else {
			am.loginAttempts[ip] = validAttempts
		}
	}
}

func (am *AuthManager) HashPassword(password string) string {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash)
}

func (am *AuthManager) CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func (am *AuthManager) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil || !am.VerifySession(cookie.Value) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (am *AuthManager) CSRFMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" {
			next(w, r)
			return
		}

		token := r.FormValue("csrf_token")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}

		if !am.VerifyCSRFToken(token) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

type PasswordError struct {
	Message string
}

func (e *PasswordError) Error() string {
	return e.Message
}
