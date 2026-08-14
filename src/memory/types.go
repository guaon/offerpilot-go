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