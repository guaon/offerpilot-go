package permission

type RiskLevel string

const (
	RiskLevelCritical RiskLevel = "criticsl"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelLow      RiskLevel = "low"
)

type PermissionRule struct {
	ToolName             string    `json:"toolName"`
	RiskLevel            RiskLevel `json:"riskLevel"`
	RequiredConfirmation bool      `json:"requiredConfirmation"`
	RateLimitPerMinute   int       `json:"rateLimitPerMinute"`
}

type PermissionDecision struct {
	Allowed             bool   `json:"allowed"`
	Reason              string `json:"reason,omitempty"`
	RequiredUserConfirm bool   `json:"requiredUserConfirm,omitempty"`
}

type AuditEntry struct {
	Timestamp int64                  `json:"timestamp"`
	SessionID string                 `json:"sessionId"`
	ToolName  string                 `json:"toolName"`
	RiskLevel RiskLevel              `json:"riskLevel"`
	Decision  string                 `json:"decision"`
	Input     map[string]interface{} `json:"input,omitempty"`
	UserID    string                 `json:"userId,omitempty"`
}
