package tool

import (
	"MyOfferPilot/src/knowledge"
)

type KnowledgeEntry struct {
	ID           int
	Title        string
	Dimension    string
	Content      string
	Question     string
	ExpertAnswer string
}

var ks *knowledge.KnowledgeSearch

func SetKnowledgeSearch(k *knowledge.KnowledgeSearch) {
	ks = k
}

func SearchKnowledgeFromDB(query, dimension string, limit int) ([]KnowledgeEntry, error) {
	if ks == nil {
		return nil, nil
	}

	results, err := ks.Search(knowledge.SearchOptions{Query: query, Dimension: dimension, Limit: limit, Method: "hybrid"})
	if err != nil {
		return nil, err
	}

	// deduplicate by source file, keep highest score per file
	seen := make(map[string]bool)
	var entries []KnowledgeEntry
	for _, r := range results {
		if seen[r.Entry.SourceFile] {
			continue
		}
		seen[r.Entry.SourceFile] = true
		entries = append(entries, KnowledgeEntry{
			Title:        r.Entry.Title,
			Dimension:    r.Entry.Dimension,
			Content:      r.Entry.Content,
			Question:     r.Entry.Question,
			ExpertAnswer: r.Entry.ExpertAnswer,
		})
		if len(entries) >= limit {
			break
		}
	}
	return entries, nil
}
