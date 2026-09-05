package diagnosis

import (
	"context"
	"database/sql"
	"fmt"
)

// Repository persists diagnosis records. Memory and profile projections
// deliberately live outside this interface.
type Repository interface {
	Save(ctx context.Context, record Record) error
	LoadAll(ctx context.Context) ([]Record, error)
}

type mysqlRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) Repository {
	if db == nil {
		return nil
	}
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) Save(ctx context.Context, record Record) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO diagnoses (user_id, session_id, dimension, score, question, timestamp) VALUES (?,?,?,?,?,?)`,
		record.UserID, record.SessionID, record.Dimension, record.Score, record.Question, record.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save diagnosis: %w", err)
	}
	return nil
}

func (r *mysqlRepository) LoadAll(ctx context.Context) ([]Record, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT user_id, session_id, dimension, score, question, timestamp FROM diagnoses ORDER BY timestamp ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("load diagnoses: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var record Record
		var sessionID, question sql.NullString
		if err := rows.Scan(&record.UserID, &sessionID, &record.Dimension, &record.Score, &question, &record.Timestamp); err != nil {
			return nil, fmt.Errorf("scan diagnosis: %w", err)
		}
		record.SessionID = sessionID.String
		record.Question = question.String
		record.ID = fmt.Sprintf("%d", record.Timestamp)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate diagnoses: %w", err)
	}
	return records, nil
}
