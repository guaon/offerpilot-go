package subagent

import queryengine "MyOfferPilot/src/query-engine"

type SubAgentRole string

const (
	SubAgentRoleDiagnostician   SubAgentRole = "diagnostician"
	SubAgentRoleInterviewer     SubAgentRole = "interviewer"
	SubAgentRoleResearcher      SubAgentRole = "researcher"
	SubAgentRoleReporter        SubAgentRole = "reporter"
	SubAgentRoleJDAnalyst       SubAgentRole = "jd-analyst"
	SubAgentRoleResumeOptimizer SubAgentRole = "resume-optimizer"
	SubAgentRoleGapAanlyzer     SubAgentRole = "gap-analyzer"
)

type SubAgentConfig struct {
	ID            string
	Role          SubAgentRole
	SystemPrompt  string
	Model         string
	MaxIterations int
	Tools         []queryengine.ToolSchema
}

type SubAgentTask struct {
	AgentId         string
	Input           string
	ParentSessionId string
	Context         string
	Timeout         int
}

type TokenUsage struct {
	Input  int
	Output int
}

type SubAgentResult struct {
	AgentId    string
	Role       SubAgentRole
	Output     string
	TokenUsage TokenUsage
	Duration   int64
	Success    bool
	Error      string
}
