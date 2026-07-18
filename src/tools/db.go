package tool

import (
	"database/sql"
	"fmt"
)

type KnowledgeEntry struct {
	ID           int
	Title        string
	Dimension    string
	Question     string
	ExpertAnswer string
}

var db *sql.DB

func InitDB(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database:%w", err)

	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database:%w", err)

	}
	return createTables()
}

func createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS knowledge_entries(
	     id INTERER PRIMARY KEY AUTOINCREMENT,
		 titile TEXT NOT NULL,
		 dimension TEXT NOT NULL,
		 question TEXT NOT NULL,
		 expert_answer TEXT NOT NULL,
		 create_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_knowledge_dimension ON knowledge_entried(dimension);
	CREATE INDEX IF NOT EXISTS idx_knowledge_index ON knowledge_entries(title);
	
	`
	_, err := db.Exec(query)
	return err

}

func SearchKnowledgeFromDB(query, dimension string, limit int) ([]KnowledgeEntry, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	baseQuery := `
	SELECT id,title,dimension,question,expert_answer
	FROM knowledge_entries
	WHERE (title LIKE ? OR question LIKE ? OR expert_answer LIKE ?)
	
	`

	params := []interface{}{
		"%" + query + "%",
		"%" + query + "%",
		"%" + query + "%",
	}

	if dimension != "" {
		baseQuery += " AND dimension = ?"
		params = append(params, dimension)
	}

	baseQuery += " ORDER BY id LIMIT ?"
	params = append(params, limit)

	rows, err := db.Query(baseQuery, params...)

	if err != nil {
		return nil, fmt.Errorf("query failed :%w", err)
	}
	defer rows.Close()

	var results []KnowledgeEntry
	for rows.Next() {
		var entry KnowledgeEntry
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.Dimension, &entry.Question, &entry.ExpertAnswer); err != nil {
			return nil, fmt.Errorf("scan failed:%w", err)

		}
		results = append(results, entry)

	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error:%w", err)
	}
	return results, nil
}

func InsertKnowledgeEntry(entry KnowledgeEntry) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	query := `
	INSERT INTO knowledge_entries (title,dimension,question,expert_answer)
	VALUES (?,?,?,?)
	`

	_, err := db.Exec(query, entry.Title, entry.Dimension, entry.Question, entry.ExpertAnswer)
	return err
}

func InitMockData() error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	for _, entry := range MockResults {
		var count int
		err := db.QueryRow("SELECT COUNT(*)FROM knowledge_entries WHERE title=?", entry.Title).Scan(&count)
		if err != nil {
			return fmt.Errorf("check existing:%w", err)
		}

		if count == 0 {
			if err := InsertKnowledgeEntry(KnowledgeEntry{
				Title:        entry.Title,
				Dimension:    entry.Dimension,
				Question:     entry.Question,
				ExpertAnswer: entry.ExpertAnswer,
			}); err != nil {
				return fmt.Errorf("insert mock data:%w", err)
			}
		}
	}

	return nil
}
