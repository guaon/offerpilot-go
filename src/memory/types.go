package memory

type MemoryType string

const (
	MemoryTypeFace MemoryType="face"
	MemoryTypePreference MemoryType="preference"
	MemoryTypeWeakness MemoryType="weakness"
	MemoryTypeStrength MemoryType="strength"
	MemoryTypeContext MemoryType="context"

)

type MemoryEntry struct {
	ID            string     `json:"id"`
	UserID        string     `json:"userId"`
	SessionID     string     `json:"sessionID"`
	Type          MemoryType `json:"type"`
	Content       string     `json:"content"`
	Importance    float64    `json:"importance"`
	CreateAt      int64      `json:"createAt"`
	LastAccessedAt int64     `json:"lastAccessedAt"`
	AccessCount   int        `json:"accessCount"`
}

type MemoryQuery struct {
	UserID        string
	SessionID     string
	Type          MemoryType
	Query         string
	Limit         int
	MinImportance float64
}

// ActiveProfile 第一层：活性画像（内存态，每轮更新）。
// 记录当前对话的面试状态机与实时表现观察。
type ActiveProfile struct {
	CurrentTopic    string   `json:"currentTopic"`    // 当前考察知识点（如 RAG）
	CurrentQuestion string   `json:"currentQuestion"` // 当前题目
	QuestionIndex   int      `json:"questionIndex"`   // 第几题（从 1 开始）
	StuckPoints     []string `json:"stuckPoints"`     // 卡顿的知识点
	ExpressionNotes []string `json:"expressionNotes"` // 表达问题（口头禅/结构乱）
	UpdatedAt       int64    `json:"updatedAt"`
}

// KnowledgePoint 第二层：知识点掌握情况（MySQL 持久化）。
type KnowledgePoint struct {
	UserID    string `json:"userId"`
	PointName string `json:"pointName"`
	Score     int    `json:"score"`
	Mastered  bool   `json:"mastered"`
	UpdatedAt int64  `json:"updatedAt"`
}

// StructuredProfile 第二层：结构化画像（MySQL 持久化）。
type StructuredProfile struct {
	UserID           string `json:"userId"`
	JobDirection     string `json:"jobDirection"`     // 求职方向
	TargetPosition   string `json:"targetPosition"`   // 目标岗位
	CurrentSituation string `json:"currentSituation"` // 当前情况
	UpdatedAt        int64  `json:"updatedAt"`
}