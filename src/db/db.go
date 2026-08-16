package db

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

// Open 建立 MySQL 连接（DSN 从环境变量 MYSQL_DSN 读取）。
func Open() (*sql.DB, error) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		return nil, fmt.Errorf("MYSQL_DSN not set")
	}

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return conn, nil
}

// Migrate 创建 6 张业务表（幂等）。
func Migrate(conn *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            VARCHAR(64) PRIMARY KEY,
			username      VARCHAR(64) NOT NULL UNIQUE,
			email         VARCHAR(255) DEFAULT NULL UNIQUE,
			password_hash VARCHAR(255) NOT NULL,
			created_at    BIGINT NOT NULL,
			updated_at    BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS auth_sessions (
			id            VARCHAR(64) PRIMARY KEY,
			user_id       VARCHAR(64) NOT NULL,
			created_at    BIGINT NOT NULL,
			expires_at    BIGINT NOT NULL,
			last_seen_at  BIGINT NOT NULL,
			INDEX idx_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS chat_sessions (
			id            VARCHAR(64) PRIMARY KEY,
			user_id       VARCHAR(64) NOT NULL DEFAULT '',
			state         VARCHAR(20) NOT NULL DEFAULT 'idle',
			metadata      TEXT NOT NULL,
			created_at    BIGINT NOT NULL,
			updated_at    BIGINT NOT NULL,
			INDEX idx_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS messages (
			id            BIGINT AUTO_INCREMENT PRIMARY KEY,
			session_id    VARCHAR(64) NOT NULL,
			role          VARCHAR(20) NOT NULL,
			content       TEXT,
			tool_call_id  VARCHAR(64),
			tool_calls    TEXT,
			created_at    BIGINT NOT NULL,
			INDEX idx_session (session_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS memories (
			id              VARCHAR(64) PRIMARY KEY,
			user_id         VARCHAR(64) NOT NULL,
			session_id      VARCHAR(64) DEFAULT NULL,
			type            VARCHAR(20) NOT NULL,
			content         TEXT NOT NULL,
			importance      DOUBLE NOT NULL DEFAULT 0,
			access_count    INT NOT NULL DEFAULT 0,
			created_at      BIGINT NOT NULL,
			last_accessed_at BIGINT NOT NULL,
			INDEX idx_user (user_id),
			INDEX idx_session (session_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS diagnoses (
			id            BIGINT AUTO_INCREMENT PRIMARY KEY,
			user_id       VARCHAR(64) NOT NULL,
			session_id    VARCHAR(64) DEFAULT NULL,
			dimension     VARCHAR(32) NOT NULL,
			score         INT NOT NULL,
			question      TEXT,
			timestamp     BIGINT NOT NULL,
			INDEX idx_user (user_id),
			INDEX idx_dimension (dimension)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS user_profiles (
			user_id           VARCHAR(64) PRIMARY KEY,
			job_direction     VARCHAR(255) DEFAULT NULL,
			target_position   VARCHAR(255) DEFAULT NULL,
			current_situation VARCHAR(512) DEFAULT NULL,
			updated_at        BIGINT NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,

		`CREATE TABLE IF NOT EXISTS knowledge_points (
			id         BIGINT AUTO_INCREMENT PRIMARY KEY,
			user_id    VARCHAR(64) NOT NULL,
			point_name VARCHAR(128) NOT NULL,
			score      INT NOT NULL,
			mastered   TINYINT NOT NULL DEFAULT 0,
			updated_at BIGINT NOT NULL,
			UNIQUE KEY uk_user_point (user_id, point_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	}

	for _, q := range queries {
		if _, err := conn.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	return nil
}
