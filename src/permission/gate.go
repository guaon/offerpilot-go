package permission

import (
	"sync"
	"time"
)

type callTracker struct {
	count       int
	windowStart int64
}

type PermissionGate struct {
	rules      map[string]*PermissionRule
	auditLog   []*AuditEntry
	callCounts map[string]*callTracker
	mu         sync.RWMutex
}

func NewPermissionGate() *PermissionGate {
	return &PermissionGate{
		rules:      make(map[string]*PermissionRule),
		auditLog:   make([]*AuditEntry, 0),
		callCounts: make(map[string]*callTracker),
	}
}

func (pg *PermissionGate) RegisterRule(rule PermissionRule) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.rules[rule.ToolName] = &rule
}

// 判断一个工具调用请求是否被允许
func (pg *PermissionGate) Check(toolName string, riskLevel RiskLevel, sessionID string) PermissionDecision {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	rule := pg.rules[toolName]

	if rule != nil && rule.RateLimitPerMinute > 0 {
		key := sessionID + ":" + toolName
		now := time.Now().UnixMilli()

		tracker := pg.callCounts[key]
		if tracker != nil && now-tracker.windowStart < 60000 {
			//// 在60秒窗口内
			if tracker.count >= rule.RateLimitPerMinute {
				//// 超过限制，拒绝
				return PermissionDecision{Allowed: false, Reason: "Rate limit exceeded"}
			}
			tracker.count++
		} else {
			pg.callCounts[key] = &callTracker{count: 1, windowStart: now}
		}
	}

	if riskLevel == RiskLevelCritical {
		return PermissionDecision{Allowed: true, RequiredUserConfirm: true}
	}

	if riskLevel == RiskLevelHigh && rule != nil && rule.RequiredConfirmation {
		return PermissionDecision{Allowed: true, RequiredUserConfirm: true}
	}

	return PermissionDecision{Allowed: true}

}

func (pg *PermissionGate) RecordAudit(entry AuditEntry) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	pg.auditLog = append(pg.auditLog, &entry)
}

func (pg *PermissionGate) GetAuditLog(sessionID string) []*AuditEntry {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	if sessionID == "" {
		result := make([]*AuditEntry, len(pg.auditLog))
		copy(result, pg.auditLog)
		return result
	}

	var result []*AuditEntry
	for _, e := range pg.auditLog {
		if e.SessionID == sessionID {
			result = append(result, e)
		}
	}
	return result
}
