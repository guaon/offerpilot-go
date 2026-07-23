package knowledge

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
)

type KnowledgeBuilder struct {
	db       *sql.DB
	search   *KnowledgeSearch
	provider EmbeddingProvider
}

func NewKnowledgeBuilder(dbPath string, provider EmbeddingProvider) (*KnowledgeBuilder, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open db:%w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping db:%w", err)
	}
	if err := CreateKnowledgeTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables:%w", err)
	}
	return &KnowledgeBuilder{
		db:       db,
		search:   NewKnowledgeSearch(db, provider),
		provider: provider,
	}, nil
}

func (kb *KnowledgeBuilder) Close() error {
	if kb.db == nil {
		return nil
	}
	return kb.db.Close()
}

func (kb *KnowledgeBuilder) BuildFromDir(dirPath string) error {
	entries, err := ParseKnowledgeDir(dirPath)
	if err != nil {
		return fmt.Errorf("parse failed:%w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no entries parsed from %s", dirPath)
	}

	if err := kb.search.BulkInsert(entries); err != nil {
		return fmt.Errorf("bulk insert failed:%w", err)
	}

	if kb.provider != nil {
		indexed, err := kb.search.IndexEmbeddings(nil)
		if err != nil {
			return fmt.Errorf("index embeddings failed:%w", err)
		}
		fmt.Printf("indexed %d embeddings\n", indexed)
	} else {
		fmt.Println("skip embeddings (no provider)")
	}
	return nil
}
