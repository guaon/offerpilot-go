package diagnosis

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Projector interface {
	Project(ctx context.Context, record Record) error
}

type ProjectorFunc func(context.Context, Record) error

func (f ProjectorFunc) Project(ctx context.Context, record Record) error {
	return f(ctx, record)
}

// Service coordinates diagnosis state, persistence, and downstream projections.
type Service struct {
	store      *Store
	repository Repository
	projector  Projector
	now        func() time.Time
}

func NewService(store *Store, repository Repository, projector Projector) *Service {
	if store == nil {
		store = NewStore()
	}
	return &Service{store: store, repository: repository, projector: projector, now: time.Now}
}

func (s *Service) Record(
	ctx context.Context,
	userID, sessionID, dimension string,
	score int,
	question string,
) (Record, error) {
	score = max(1, min(10, score))
	now := s.now()
	record := Record{
		ID:        fmt.Sprintf("%d", now.UnixNano()),
		Timestamp: now.UnixMilli(),
		Dimension: dimension,
		Score:     score,
		Question:  question,
		SessionID: sessionID,
		UserID:    userID,
	}

	s.store.Add(record)

	var result error
	if s.repository != nil {
		if err := s.repository.Save(ctx, record); err != nil {
			result = errors.Join(result, err)
		}
	}
	if s.projector != nil {
		if err := s.projector.Project(ctx, record); err != nil {
			result = errors.Join(result, fmt.Errorf("project diagnosis: %w", err))
		}
	}
	return record, result
}

func (s *Service) Load(ctx context.Context) (int, error) {
	if s.repository == nil {
		return 0, nil
	}
	records, err := s.repository.LoadAll(ctx)
	if err != nil {
		return 0, err
	}
	s.store.Load(records)
	return len(records), nil
}
