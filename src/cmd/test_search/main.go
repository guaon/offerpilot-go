package main

import (
	"encoding/json"
	"fmt"
	"MyOfferPilot/src/env"
	"MyOfferPilot/src/knowledge"
)

func main() {
	env.LoadEnvFile()

	ks := knowledge.NewKnowledgeSearchFromFile("./data/knowledge.db")
	if ks == nil {
		fmt.Println("open db failed")
		return
	}

	queries := []string{
		"context-window.md 滑动窗口 摘要 结构化记忆",
		"Agent 记忆模块 Memory 短期长期记忆 架构设计",
		"滑动窗口 摘要记忆 混合检索 对话管理策略",
	}

	for _, q := range queries {
		fmt.Printf("\n=== Query: %s ===\n", q)

		results, err := ks.Search(knowledge.SearchOptions{Query: q, Limit: 3})
		if err != nil {
			fmt.Printf("search error: %v\n", err)
		} else {
			fmt.Printf("results: %d\n", len(results))
			for i, r := range results {
				fmt.Printf("  [%d] score=%.3f title=%s question=%q\n", i, r.Score, r.Entry.Title, r.Entry.Question)
				fmt.Printf("       content_len=%d expertAnswer_len=%d\n", len(r.Entry.Content), len(r.Entry.ExpertAnswer))
				b, _ := json.Marshal(r)
				fmt.Printf("  JSON snippet: %.200s\n", string(b))
			}
		}
	}
}
