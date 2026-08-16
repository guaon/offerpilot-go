package user

import (
	"database/sql"
	"fmt"
)

type User struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    int64
	UpdatedAt    int64
}

type AuthSession struct {
	ID         string
	UserID     string
	CreatedAt  int64
	ExpiresAt  int64
	LastSeenAt int64
}

// Store 封装 users 与 auth_sessions 两张表的数据访问。
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateUser 插入新用户。username/email 唯一冲突时返回错误。空 email 存 NULL。
func (s *Store) CreateUser(id, username, email, passwordHash string, now int64) (*User, error) {
	var emailArg interface{}
	if email == "" {
		emailArg = nil
	} else {
		emailArg = email
	}

	_, err := s.db.Exec(
		`INSERT INTO users (id, username, email, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, username, emailArg, passwordHash, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return &User{
		ID:           id,
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	return s.queryUser(`SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE username = ?`, username)
}

func (s *Store) GetUserByEmail(email string) (*User, error) {
	return s.queryUser(`SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE email = ?`, email)
}

func (s *Store) GetUserByID(id string) (*User, error) {
	return s.queryUser(`SELECT id, username, email, password_hash, created_at, updated_at
		FROM users WHERE id = ?`, id)
}

func (s *Store) queryUser(query string, arg string) (*User, error) {
	var u User
	var email sql.NullString
	err := s.db.QueryRow(query, arg).Scan(
		&u.ID, &u.Username, &email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	u.Email = email.String
	return &u, nil
}

// CreateAuthSession 写入一条登录会话。
func (s *Store) CreateAuthSession(id, userID string, createdAt, expiresAt int64) error {
	_, err := s.db.Exec(
		`INSERT INTO auth_sessions (id, user_id, created_at, expires_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, userID, createdAt, expiresAt, createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert auth session: %w", err)
	}
	return nil
}

func (s *Store) GetAuthSession(id string) (*AuthSession, error) {
	var a AuthSession
	err := s.db.QueryRow(
		`SELECT id, user_id, created_at, expires_at, last_seen_at FROM auth_sessions WHERE id = ?`,
		id,
	).Scan(&a.ID, &a.UserID, &a.CreatedAt, &a.ExpiresAt, &a.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query auth session: %w", err)
	}
	return &a, nil
}

func (s *Store) DeleteAuthSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM auth_sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}
