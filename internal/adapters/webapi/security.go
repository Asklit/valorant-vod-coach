package webapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

const csrfHeaderName = "X-CSRF-Token"

type authRateLimitEntry struct {
	windowStart time.Time
	count       int
}

type authRateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	entries   map[string]authRateLimitEntry
	lastSweep time.Time
}

func newAuthRateLimiter(limit int, window time.Duration) *authRateLimiter {
	return &authRateLimiter{limit: limit, window: window, entries: map[string]authRateLimitEntry{}}
}

func (l *authRateLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	if l == nil || l.limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastSweep.IsZero() || now.Sub(l.lastSweep) >= l.window {
		for entryKey, candidate := range l.entries {
			if now.Sub(candidate.windowStart) >= l.window {
				delete(l.entries, entryKey)
			}
		}
		l.lastSweep = now
	}
	entry := l.entries[key]
	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		entry = authRateLimitEntry{windowStart: now, count: 1}
		l.entries[key] = entry
		return true, 0
	}
	if entry.count >= l.limit {
		return false, l.window - now.Sub(entry.windowStart)
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}

func (s *Server) allowAuthRequest(w http.ResponseWriter, r *http.Request, action string) bool {
	allowed, retryAfter := s.authRate.allow(action+":"+clientIP(r), time.Now().UTC())
	if allowed {
		return true
	}
	seconds := max(1, int(retryAfter.Seconds()+0.999))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, errors.New("too many authentication requests"))
	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	return r.RemoteAddr
}

func setSecurityHeaders(header http.Header) {
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	header.Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'; img-src 'self' data: blob:; media-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' http://127.0.0.1:* http://localhost:*")
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func (s *Server) requiresCSRF(r *http.Request) bool {
	if !isUnsafeMethod(r.Method) {
		return false
	}
	switch r.URL.Path {
	case "/api/auth/register", "/api/auth/login":
		return false
	default:
		return s.requiresAPIAuth(r)
	}
}

func (s *Server) hasValidCSRFToken(r *http.Request) bool {
	session, ok := s.currentSession(r)
	if !ok {
		return false
	}
	provided := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if provided == "" || len(provided) != len(session.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) == 1
}

func (s *Server) isTrustedRequestOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if isAllowedDevOrigin(origin) {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := "http"
	if requestUsesHTTPS(r) {
		scheme = "https"
	}
	return strings.EqualFold(parsed.Scheme, scheme) && strings.EqualFold(parsed.Host, r.Host)
}

func (s *Server) requireVODAccess(w http.ResponseWriter, r *http.Request, label string) (app.PublicAuthUser, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return app.PublicAuthUser{}, false
	}
	label = strings.TrimSpace(label)
	if !isSafeResourceID(label) {
		writeError(w, http.StatusNotFound, errors.New("VOD not found"))
		return app.PublicAuthUser{}, false
	}
	if _, _, err := s.vodResolver(user.ID, user.Role == app.AuthRoleAdmin).ResolveVOD(r.Context(), label); err != nil {
		writeError(w, http.StatusNotFound, errors.New("VOD not found"))
		return app.PublicAuthUser{}, false
	}
	return user, true
}

func (s *Server) adminHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.requireAdmin(w, r); !ok {
			return
		}
		next(w, r)
	}
}

func (s *Server) adminHTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.requireAdmin(w, r); !ok {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeResourceID(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) analysisRoot(userID string) string {
	if !isSafeResourceID(userID) {
		return filepath.Join(s.config.ProcessedRoot, "users", "invalid", "analyses")
	}
	return filepath.Join(s.config.ProcessedRoot, "users", userID, "analyses")
}

func (s *Server) handleArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/artifacts/")
	target, parts, err := secureArtifactPath(s.config.ProcessedRoot, relative)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	if !s.canReadArtifact(r.Context(), user, parts) {
		writeError(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}

	file, err := os.Open(target)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, errors.New("artifact not found"))
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func secureArtifactPath(root string, rawRelative string) (string, []string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	relative := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rawRelative)))
	if relative == "." || relative == "" || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", nil, errors.New("invalid artifact path")
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, relative))
	if err != nil {
		return "", nil, err
	}
	inside, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", nil, errors.New("artifact path escapes root")
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", nil, err
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return "", nil, err
	}
	inside, err = filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return "", nil, errors.New("artifact symlink escapes root")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", nil, errors.New("invalid artifact path")
	}
	return resolvedTarget, parts, nil
}

func (s *Server) canReadArtifact(ctx context.Context, user app.PublicAuthUser, parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	if user.Role == app.AuthRoleAdmin {
		return true
	}
	switch parts[0] {
	case "users":
		if len(parts) < 4 || parts[1] != user.ID || parts[2] != "analyses" || !isSafeResourceID(parts[3]) {
			return false
		}
		_, _, err := s.vodResolver(user.ID, false).ResolveVOD(ctx, parts[3])
		return err == nil
	case "evaluations", "benchmarks", "corrections", "coaching":
		return false
	default:
		if !isSafeResourceID(parts[0]) {
			return false
		}
		_, _, err := s.vodResolver(user.ID, false).ResolveVOD(ctx, parts[0])
		return err == nil
	}
}

func artifactURLForPath(root string, path string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("artifact path is outside processed root")
	}
	return "/artifacts/" + strings.TrimPrefix(filepath.ToSlash(relative), "/"), nil
}
