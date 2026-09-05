package main

import (
	"flag"
	"fmt"
	"os"

	"MyOfferPilot/src/env"
	"MyOfferPilot/src/knowledge"
)

func main() {
	skipEmbeddings := flag.Bool("skip-embeddings", false, "Skip embedding generation (useful when API quota is exhausted)")
	flag.Parse()

	env.LoadEnvFile()

	knowledgeDir := flag.Arg(0)
	if knowledgeDir == "" {
		fmt.Println("Usage: build_kb [--skip-embeddings] <knowledge_dir>")
		os.Exit(1)
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if *skipEmbeddings {
		apiKey = ""
		fmt.Println("Skipping embeddings (--skip-embeddings)")
	} else if apiKey == "" {
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

	if err := builder.Clear(); err != nil {
		fmt.Printf("Failed to clear knowledge base: %v\n", err)
		os.Exit(1)
	}

	if err := builder.BuildFromDir(knowledgeDir); err != nil {
		fmt.Printf("Failed to build knowledge base: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Knowledge base built successfully!")
}