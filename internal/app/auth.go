package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	AuthRoleAdmin = "admin"
	AuthRoleUser  = "user"
)

type AuthStore struct {
	Path       string
	Clock      func() time.Time
	Iterations int
	mu         sync.Mutex
}

type AuthRegisterRequest struct {
	Email       string
	Password    string
	DisplayName string
}

type AuthLoginRequest struct {
	Email    string
	Password string
}

type AuthUser struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	DisplayName  string     `json:"display_name"`
	Role         string     `json:"role"`
	PasswordHash string     `json:"password_hash"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

type PublicAuthUser struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type authUserFile struct {
	SchemaVersion int        `json:"schema_version"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Users         []AuthUser `json:"users"`
}

func (s *AuthStore) Register(ctx context.Context, request AuthRegisterRequest) (PublicAuthUser, error) {
	if err := ctx.Err(); err != nil {
		return PublicAuthUser{}, err
	}
	if strings.TrimSpace(s.Path) == "" {
		return PublicAuthUser{}, errors.New("auth store path is required")
	}

	email, displayName, password, err := normalizeAuthRegistration(request)
	if err != nil {
		return PublicAuthUser{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.load()
	if err != nil {
		return PublicAuthUser{}, err
	}
	for _, user := range file.Users {
		if user.Email == email {
			return PublicAuthUser{}, fmt.Errorf("user already exists: %s", email)
		}
	}

	now := s.now()
	role := AuthRoleUser
	if len(file.Users) == 0 {
		role = AuthRoleAdmin
	}
	passwordHash, err := hashPassword(password, s.iterations())
	if err != nil {
		return PublicAuthUser{}, err
	}
	user := AuthUser{
		ID:           newAuthID("user"),
		Email:        email,
		DisplayName:  displayName,
		Role:         role,
		PasswordHash: passwordHash,
		CreatedAt:    now,
	}
	file.Users = append(file.Users, user)
	if err := s.save(file); err != nil {
		return PublicAuthUser{}, err
	}
	return publicAuthUser(user), nil
}

func (s *AuthStore) Authenticate(ctx context.Context, request AuthLoginRequest) (PublicAuthUser, error) {
	if err := ctx.Err(); err != nil {
		return PublicAuthUser{}, err
	}
	email := normalizeEmail(request.Email)
	if email == "" || strings.TrimSpace(request.Password) == "" {
		return PublicAuthUser{}, errors.New("email and password are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.load()
	if err != nil {
		return PublicAuthUser{}, err
	}
	for index := range file.Users {
		user := &file.Users[index]
		if user.Email != email {
			continue
		}
		if !verifyPassword(request.Password, user.PasswordHash) {
			return PublicAuthUser{}, errors.New("invalid email or password")
		}
		now := s.now()
		user.LastLoginAt = &now
		if err := s.save(file); err != nil {
			return PublicAuthUser{}, err
		}
		return publicAuthUser(*user), nil
	}
	return PublicAuthUser{}, errors.New("invalid email or password")
}

func (s *AuthStore) ListUsers(ctx context.Context) ([]PublicAuthUser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	users := make([]PublicAuthUser, 0, len(file.Users))
	for _, user := range file.Users {
		users = append(users, publicAuthUser(user))
	}
	return users, nil
}

func (s *AuthStore) UserCount(ctx context.Context) (int, error) {
	users, err := s.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	return len(users), nil
}

func (s *AuthStore) load() (authUserFile, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return authUserFile{SchemaVersion: 1, Users: []AuthUser{}}, nil
		}
		return authUserFile{}, err
	}
	var file authUserFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return authUserFile{}, err
	}
	if file.SchemaVersion == 0 {
		file.SchemaVersion = 1
	}
	if file.Users == nil {
		file.Users = []AuthUser{}
	}
	return file, nil
}

func (s *AuthStore) save(file authUserFile) error {
	file.SchemaVersion = 1
	file.UpdatedAt = s.now()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(s.Path, raw, 0o600)
}

func (s *AuthStore) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s *AuthStore) iterations() int {
	if s.Iterations > 0 {
		return s.Iterations
	}
	return 120000
}

func normalizeAuthRegistration(request AuthRegisterRequest) (string, string, string, error) {
	email := normalizeEmail(request.Email)
	password := strings.TrimSpace(request.Password)
	displayName := strings.TrimSpace(request.DisplayName)
	if email == "" || !strings.Contains(email, "@") {
		return "", "", "", errors.New("valid email is required")
	}
	if len(password) < 8 {
		return "", "", "", errors.New("password must be at least 8 characters")
	}
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	return email, displayName, password, nil
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func publicAuthUser(user AuthUser) PublicAuthUser {
	return PublicAuthUser{
		ID:          user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role,
		CreatedAt:   user.CreatedAt,
		LastLoginAt: user.LastLoginAt,
	}
}

func hashPassword(password string, iterations int) (string, error) {
	salt, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	hash := pbkdf2Key([]byte(password), salt, iterations, 32, sha256.New)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s",
		iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func verifyPassword(password string, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := strconvAtoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	actual := pbkdf2Key([]byte(password), salt, iterations, len(expected), sha256.New)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func strconvAtoi(value string) (int, error) {
	var result int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid integer: %s", value)
		}
		result = result*10 + int(r-'0')
	}
	return result, nil
}

func pbkdf2Key(password []byte, salt []byte, iterations int, keyLen int, newHash func() hash.Hash) []byte {
	hashLen := newHash().Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	output := make([]byte, 0, numBlocks*hashLen)
	var blockIndex [4]byte
	for block := 1; block <= numBlocks; block++ {
		binary.BigEndian.PutUint32(blockIndex[:], uint32(block))
		mac := hmac.New(newHash, password)
		mac.Write(salt)
		mac.Write(blockIndex[:])
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(newHash, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		output = append(output, t...)
	}
	return output[:keyLen]
}

func randomBytes(length int) ([]byte, error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func NewAuthToken() (string, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newAuthID(prefix string) string {
	token, err := NewAuthToken()
	if err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	if len(token) > 18 {
		token = token[:18]
	}
	return prefix + "_" + token
}
