package knowledge

type MatchType string

const (
	MatchTypeFTS       MatchType = "fts"
	MatchTypeEmbedding MatchType = "embedding"
	MatchTypeHybrid    MatchType = "hybrid"
)

type KnowledgeEntry struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Dimension    string   `json:"dimension"`
	Content      string   `json:"content"`
	SourceFile   string   `json:"sourceFile,omitempty"`
	Question     string   `json:"question,omitempty"`
	ExpertAnswer string   `json:"expertAnswer,omitempty"`
	NoviceAnswer string   `json:"noviceAnswer"`
	Tags         []string `json:"tag,omitempty"`
}

type SearchResult struct {
	Entry     *KnowledgeEntry `json:"entry"`
	Score     float64         `json:"score"`
	MatchType MatchType       `json:"matchType"`
}

type SearchOptions struct {
	Query     string  `json:"query"`
	Dimension string  `json:"dimension,omitempty"`
	Limit     int  `json:"limit"`
	MinScore  float64 `json:"minScore,omitempty"`
	Method    string  `json:"method,omitempty"`
}
