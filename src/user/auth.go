package user

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUsernameTaken      = errors.New("用户名已被占用")
	ErrEmailTaken         = errors.New("邮箱已被注册")
)

// Service 封装认证业务逻辑（密码哈希、注册、登录、会话）。
type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Register 注册新用户。用户名/邮箱冲突返回对应错误。
func (s *Service) Register(username, email, password string) (*User, error) {
	if u, _ := s.store.GetUserByUsername(username); u != nil {
		return nil, ErrUsernameTaken
	}
	if email != "" {
		if u, _ := s.store.GetUserByEmail(email); u != nil {
			return nil, ErrEmailTaken
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	return s.store.CreateUser(uuid.New().String(), username, email, string(hash), now)
}

// Login 按用户名或邮箱验证密码，返回用户。
func (s *Service) Login(usernameOrEmail, password string) (*User, error) {
	u, _ := s.store.GetUserByUsername(usernameOrEmail)
	if u == nil {
		u, _ = s.store.GetUserByEmail(usernameOrEmail)
	}
	if u == nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

// CreateSession 创建登录会话，返回 session ID（默认 30 天有效）。
func (s *Service) CreateSession(userID string) (string, error) {
	id := uuid.New().String()
	now := time.Now().UnixMilli()
	expires := now + 30*24*3600*1000 // 30 天
	if err := s.store.CreateAuthSession(id, userID, now, expires); err != nil {
		return "", err
	}
	return id, nil
}

// Logout 删除登录会话。
func (s *Service) Logout(sessionID string) error {
	return s.store.DeleteAuthSession(sessionID)
}

// ValidateSession 校验会话有效性，返回用户。过期或不存在返回错误。
func (s *Service) ValidateSession(sessionID string) (*User, error) {
	sess, err := s.store.GetAuthSession(sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, errors.New("session not found")
	}
	if sess.ExpiresAt < time.Now().UnixMilli() {
		return nil, errors.New("session expired")
	}
	return s.store.GetUserByID(sess.UserID)
}

// GetUserByID 按 ID 查用户。
func (s *Service) GetUserByID(id string) (*User, error) {
	return s.store.GetUserByID(id)
}
