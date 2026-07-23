package main

import (
	"fmt"
	"os"

	"MyOfferPilot/src/env"
	"MyOfferPilot/src/knowledge"
)

func main() {
	env.LoadEnvFile()

	if len(os.Args) < 2 {
		fmt.Println("Usage: build_kb <knowledge_dir>")
		os.Exit(1)
	}

	knowledgeDir := os.Args[1]

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("Warning: OPENAI_API_KEY not set, embeddings will be skipped")
	}

	var provider knowledge.EmbeddingProvider
	if apiKey != "" {
		provider = knowledge.NewOpenAIEmbeddingProvider()
	}

	builder, err := knowledge.NewKnowledgeBuilder("./data/knowledge.db", provider)
	if err != nil {
		fmt.Printf("Failed to create builder: %v\n", err)
		os.Exit(1)
	}
	defer builder.Close()

	if err := builder.BuildFromDir(knowledgeDir); err != nil {
		fmt.Printf("Failed to build knowledge base: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Knowledge base built successfully!")
}
