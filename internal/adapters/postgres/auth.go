package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

func (s Store) Register(ctx context.Context, request app.AuthRegisterRequest) (app.PublicAuthUser, error) {
	if s.DB == nil {
		return app.PublicAuthUser{}, errors.New("postgres store requires DB")
	}
	email, displayName, password, err := app.NormalizeAuthRegistration(request)
	if err != nil {
		return app.PublicAuthUser{}, err
	}
	passwordHash, err := app.HashAuthPassword(password, s.AuthHashIterations)
	if err != nil {
		return app.PublicAuthUser{}, err
	}

	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return app.PublicAuthUser{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "LOCK TABLE auth_users IN EXCLUSIVE MODE"); err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("lock auth users: %w", err)
	}
	var userCount int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM auth_users").Scan(&userCount); err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("count auth users: %w", err)
	}
	if userCount == 0 && !request.BootstrapAdmin {
		return app.PublicAuthUser{}, errors.New("administrator setup is required")
	}
	var duplicate bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM auth_users WHERE lower(email) = lower($1))", email).Scan(&duplicate); err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("check auth user: %w", err)
	}
	if duplicate {
		return app.PublicAuthUser{}, fmt.Errorf("user already exists: %s", email)
	}

	role := app.AuthRoleUser
	if userCount == 0 && request.BootstrapAdmin {
		role = app.AuthRoleAdmin
	}
	user := app.AuthUser{
		ID:           app.NewAuthUserID(),
		Email:        email,
		DisplayName:  displayName,
		Role:         role,
		PasswordHash: passwordHash,
		CreatedAt:    s.now(),
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_users (id, email, display_name, role, password_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
`, user.ID, user.Email, user.DisplayName, user.Role, user.PasswordHash, user.CreatedAt); err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("insert auth user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return app.PublicAuthUser{}, err
	}
	return app.PublicAuthUserFromRecord(user), nil
}

func (s Store) Authenticate(ctx context.Context, request app.AuthLoginRequest) (app.PublicAuthUser, error) {
	if s.DB == nil {
		return app.PublicAuthUser{}, errors.New("postgres store requires DB")
	}
	email := app.NormalizeAuthEmail(request.Email)
	if email == "" || strings.TrimSpace(request.Password) == "" {
		return app.PublicAuthUser{}, errors.New("email and password are required")
	}
	var user app.AuthUser
	var lastLogin sql.NullTime
	var disabled sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
SELECT id, email, display_name, role, password_hash, created_at, last_login_at, disabled_at
FROM auth_users
WHERE lower(email) = lower($1)
`, email).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.Role,
		&user.PasswordHash,
		&user.CreatedAt,
		&lastLogin,
		&disabled,
	)
	if errors.Is(err, sql.ErrNoRows) || disabled.Valid {
		return app.PublicAuthUser{}, errors.New("invalid email or password")
	}
	if err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("find auth user: %w", err)
	}
	if !app.VerifyAuthPassword(request.Password, user.PasswordHash) {
		return app.PublicAuthUser{}, errors.New("invalid email or password")
	}
	if lastLogin.Valid {
		value := lastLogin.Time.UTC()
		user.LastLoginAt = &value
	}
	now := s.now()
	if _, err := s.DB.ExecContext(ctx, `
UPDATE auth_users SET last_login_at = $2, updated_at = now() WHERE id = $1
`, user.ID, now); err != nil {
		return app.PublicAuthUser{}, fmt.Errorf("update last login: %w", err)
	}
	user.LastLoginAt = &now
	return app.PublicAuthUserFromRecord(user), nil
}

func (s Store) ListUsers(ctx context.Context) ([]app.PublicAuthUser, error) {
	if s.DB == nil {
		return nil, errors.New("postgres store requires DB")
	}
	rows, err := s.DB.QueryContext(ctx, `
SELECT id, email, display_name, role, created_at, last_login_at
FROM auth_users
WHERE disabled_at IS NULL
ORDER BY created_at, id
`)
	if err != nil {
		return nil, fmt.Errorf("list auth users: %w", err)
	}
	defer rows.Close()
	users := make([]app.PublicAuthUser, 0)
	for rows.Next() {
		var user app.PublicAuthUser
		var lastLogin sql.NullTime
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Role, &user.CreatedAt, &lastLogin); err != nil {
			return nil, fmt.Errorf("scan auth user: %w", err)
		}
		if lastLogin.Valid {
			value := lastLogin.Time.UTC()
			user.LastLoginAt = &value
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auth users: %w", err)
	}
	return users, nil
}

func (s Store) UserCount(ctx context.Context) (int, error) {
	if s.DB == nil {
		return 0, errors.New("postgres store requires DB")
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM auth_users WHERE disabled_at IS NULL").Scan(&count); err != nil {
		return 0, fmt.Errorf("count auth users: %w", err)
	}
	return count, nil
}

var _ app.Authenticator = Store{}
