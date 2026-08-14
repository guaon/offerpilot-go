# 登录认证与三层记忆系统设计

日期：2026-08-14
状态：待审查

## 1. 背景与目标

OfferPilot 目前无用户概念：所有存储用 SQLite，记忆系统（MemoryStore）只有查询从未写入，诊断记录存内存（重启丢失），"新会话"仅清空消息。目标是支持多人部署，引入登录认证，并让记忆跨会话保留。

**目标**：
- 邮箱/用户名 + 密码登录（Session + Cookie 方案）
- 用户级数据落 MySQL，共享只读数据留 SQLite
- 三层记忆：用户画像（永久）+ 面试总结（跨会话）+ 会话记忆（随会话）
- 诊断记录从内存落库，解决重启丢失

## 2. 技术决策

| 决策点 | 结论 | 理由 |
|--------|------|------|
| 数据库 | MySQL（用户级数据）+ SQLite（知识库） | 多人并发写需要 MySQL；FTS5 中文检索是 SQLite 强项，知识库只读共享 |
| 认证机制 | Session + Cookie | 延续现有 offerpilot_sid 模式，可主动注销，前端零改动 |
| 数据访问 | database/sql 手写 SQL | 与现有三个存储层风格一致，换驱动名即可迁移 |
| 密码哈希 | bcrypt | 已存在于 golang.org/x/crypto |
| MySQL 驱动 | go-sql-driver/mysql | 最主流 |
| 记忆写入① | 诊断后规则化（score 阈值） | 零 LLM 成本 |
| 记忆写入② | 新会话时 LLM 总结 | 复用现有 queryEngine |
| 画像提取 | 第一版规则化，LLM 画像留待下一版 | YAGNI |

## 3. 数据库设计（MySQL，6 张表）

统一 UUID 字符串 ID，时间戳 BIGINT 存 UnixMilli（与现有 Go 代码一致）。

```sql
-- ① 用户表
CREATE TABLE users (
    id            VARCHAR(64) PRIMARY KEY,
    username      VARCHAR(64) NOT NULL UNIQUE,
    email         VARCHAR(255) DEFAULT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL
);

-- ② 登录会话表
CREATE TABLE auth_sessions (
    id            VARCHAR(64) PRIMARY KEY,
    user_id       VARCHAR(64) NOT NULL,
    created_at    BIGINT NOT NULL,
    expires_at    BIGINT NOT NULL,
    last_seen_at  BIGINT NOT NULL,
    INDEX idx_user (user_id)
);

-- ③ 对话会话表（迁移 sessions.db）
CREATE TABLE chat_sessions (
    id            VARCHAR(64) PRIMARY KEY,
    user_id       VARCHAR(64) NOT NULL DEFAULT '',
    state         VARCHAR(20) NOT NULL DEFAULT 'idle',
    metadata      TEXT NOT NULL,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL,
    INDEX idx_user (user_id)
);

-- ④ 消息表
CREATE TABLE messages (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    session_id    VARCHAR(64) NOT NULL,
    role          VARCHAR(20) NOT NULL,
    content       TEXT,
    tool_call_id  VARCHAR(64),
    tool_calls    TEXT,
    created_at    BIGINT NOT NULL,
    INDEX idx_session (session_id)
);

-- ⑤ 记忆表（加 user_id 跨会话）
CREATE TABLE memories (
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
);

-- ⑥ 诊断记录表（从内存落库）
CREATE TABLE diagnoses (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id       VARCHAR(64) NOT NULL,
    session_id    VARCHAR(64) DEFAULT NULL,
    dimension     VARCHAR(32) NOT NULL,
    score         INT NOT NULL,
    question      TEXT,
    timestamp     BIGINT NOT NULL,
    INDEX idx_user (user_id),
    INDEX idx_dimension (dimension)
);
```

## 4. 认证流程

### API 端点

| 端点 | 方法 | 认证 |
|------|------|------|
| /api/register | POST | 无 |
| /api/login | POST | 无 |
| /api/logout | POST | 需登录 |
| /api/me | GET | 需登录 |

### Cookie

| Cookie | 值 | 用途 |
|--------|-----|------|
| offerpilot_auth | auth_session UUID | 登录态（新增） |
| offerpilot_sid | chat_session UUID | 对话态（现有） |

### 中间件

requireAuth 包裹以下业务 API：`/api/chat`、`/api/session`、`/api/diagnosis`、`/api/interview`、`/api/match`、`/api/resume`、`/api/transcribe`、`/api/tts`、`/api/parse-pdf`、`/api/parse-url`。从 cookie 读 offerpilot_auth → 查 auth_sessions 验证未过期 → userId 放入请求上下文。未登录返回 401。`/health`、`/api/config`（仅 GET）保持公开。

### 登录与对话关联

登录成功读 cookie offerpilot_sid，UPDATE chat_sessions SET user_id = 登录用户 WHERE id = sid（登录前匿名对话认领）。

### API Key 旁路

.env 的 OFFERPILOT_API_KEY 保留，作为程序化调用（绕过登录）的通道，与 session 认证共存。

## 5. 三层记忆

### 层级映射

| 层级 | MemoryType | 生命周期 |
|------|-----------|---------|
| 用户画像 | face + preference | 永久（user 级） |
| 面试总结 | weakness + strength | 跨会话（user 级） |
| 会话记忆 | context | 随会话（session 级） |

### 写入时机

- **触发点① 诊断后（规则化）**：record_diagnosis 工具调用时，score < 6 写 weakness，score >= 7 写 strength，同时写 diagnoses 表。
- **触发点② 新会话时（LLM 总结）**：挂在 `POST /api/session` 创建新会话的时机上——若存在旧会话（cookie offerpilot_sid 对应的 chat_session 有消息），先取旧会话 messages + 诊断记录调 LLM 生成总结写 weakness/strength，提取画像写 face/preference，旧会话标记 completed，再创建新 chat_session。前端已有"重置"按钮（onReset）触发此流程，无需新加按钮。

### 加载时机

Agent.Run 开始时按 user_id 查询记忆，注入 ContextManager memory layer，System Prompt 附带画像与面试总结。

## 6. 代码结构

### 新增文件

| 文件 | 职责 |
|------|------|
| src/db/db.go | MySQL 连接、建表 |
| src/user/store.go | users + auth_sessions CRUD |
| src/user/auth.go | bcrypt、注册/登录/登出逻辑 |
| src/server/auth.go | 认证 handler + requireAuth |

### 修改文件

| 文件 | 改动 |
|------|------|
| main.go | 初始化 MySQL |
| app/app.go | 注入 userStore，memStore 用 MySQL |
| session/manager.go | sql.Open 驱动改 mysql，SQL 语法微调 |
| memory/store.go | 同上 + MemoryEntry 加 UserID |
| agent/loop.go | 记忆查询 SessionID → UserID |
| server/server.go | 业务路由包 requireAuth，recordDiagnosis 落库 |

### 环境变量

```
MYSQL_DSN=root:password@tcp(127.0.0.1:3306)/offerpilot?parseTime=true&charset=utf8mb4
```

### SQL 语法差异

| SQLite | MySQL |
|--------|-------|
| INSERT OR REPLACE | INSERT ... ON DUPLICATE KEY UPDATE |
| AUTOINCREMENT | AUTO_INCREMENT |

## 7. 数据迁移

- 现有 sessions.db 历史数据不迁移（单机测试数据，无价值），新系统从空 MySQL 库开始。
- 知识库 knowledge.db（含 FTS5 + 向量）不动，留在 SQLite。
- 诊断记录从内存搬到 MySQL diagnoses 表。

## 8. 实现范围（第一版）

包含：MySQL 接入 + 6 张表、注册/登录/登出/me、requireAuth、会话与消息迁 MySQL、记忆加 user_id 并按 user_id 查询、诊断记录落库、新会话按钮（前端）。

不包含：LLM 深度画像（留规则化）、历史数据迁移、OAuth/第三方登录、token 刷新机制。
