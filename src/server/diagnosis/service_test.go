package diagnosis

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	saved   []Record
	loaded  []Record
	saveErr error
	loadErr error
}

func (r *fakeRepository) Save(_ context.Context, record Record) error {
	r.saved = append(r.saved, record)
	return r.saveErr
}

func (r *fakeRepository) LoadAll(context.Context) ([]Record, error) {
	return r.loaded, r.loadErr
}

func TestServiceRecordNormalizesAndPersists(t *testing.T) {
	store := NewStore()
	repository := &fakeRepository{}
	var projected Record
	service := NewService(store, repository, ProjectorFunc(
		func(_ context.Context, record Record) error {
			projected = record
			return nil
		},
	))
	service.now = func() time.Time { return time.Unix(123, 456_000_000) }

	record, err := service.Record(context.Background(), "user-1", "session-1", "rag", 99, "question")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if record.Score != 10 || record.Timestamp != 123456 || record.ID != "123456000000" {
		t.Fatalf("unexpected normalized record: %#v", record)
	}
	if len(repository.saved) != 1 || repository.saved[0] != record {
		t.Fatalf("repository saved %#v, want %#v", repository.saved, record)
	}
	if projected != record {
		t.Fatalf("projected %#v, want %#v", projected, record)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}

	low, err := service.Record(context.Background(), "user-1", "session-1", "model", -1, "question")
	if err != nil {
		t.Fatalf("Record low score: %v", err)
	}
	if low.Score != 1 {
		t.Fatalf("low score = %d, want 1", low.Score)
	}
}

func TestServiceContinuesAfterRepositoryFailure(t *testing.T) {
	saveErr := errors.New("database unavailable")
	projectErr := errors.New("projection unavailable")
	store := NewStore()
	repository := &fakeRepository{saveErr: saveErr}
	projected := false
	service := NewService(store, repository, ProjectorFunc(
		func(context.Context, Record) error {
			projected = true
			return projectErr
		},
	))

	_, err := service.Record(context.Background(), "user-1", "session-1", "engineering", 5, "question")
	if !errors.Is(err, saveErr) || !errors.Is(err, projectErr) {
		t.Fatalf("Record error = %v, want persistence and projection errors", err)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}
	if !projected {
		t.Fatal("projector was not called after repository failure")
	}
}

func TestServiceLoadRebuildsStore(t *testing.T) {
	store := NewStore()
	store.Add(Record{UserID: "stale", Dimension: "old", Score: 1})
	repository := &fakeRepository{loaded: []Record{
		{ID: "1", UserID: "user-1", Dimension: "rag", Score: 4},
		{ID: "2", UserID: "user-1", Dimension: "model", Score: 8},
	}}
	service := NewService(store, repository, nil)

	count, err := service.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if count != 2 || store.Count() != 2 {
		t.Fatalf("loaded count = %d, store count = %d", count, store.Count())
	}
	stale, _ := store.Snapshot("stale", "")
	if len(stale) != 0 {
		t.Fatalf("stale records were retained: %#v", stale)
	}
	records, states := store.Snapshot("user-1", "")
	if len(records) != 2 || len(states) != 2 {
		t.Fatalf("rebuilt records = %d, states = %d", len(records), len(states))
	}
}

func TestServiceSupportsMemoryOnlyMode(t *testing.T) {
	store := NewStore()
	service := NewService(store, nil, nil)

	if _, err := service.Record(context.Background(), "", "session-1", "architecture", 6, "question"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if count, err := service.Load(context.Background()); err != nil || count != 0 {
		t.Fatalf("Load = %d, %v", count, err)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want 1", store.Count())
	}
}

func TestServiceLoadErrorLeavesStoreUntouched(t *testing.T) {
	loadErr := errors.New("database unavailable")
	store := NewStore()
	store.Add(Record{UserID: "user-1", Dimension: "rag", Score: 5})
	service := NewService(store, &fakeRepository{loadErr: loadErr}, nil)

	if _, err := service.Load(context.Background()); !errors.Is(err, loadErr) {
		t.Fatalf("Load error = %v, want %v", err, loadErr)
	}
	if store.Count() != 1 {
		t.Fatalf("store count = %d, want existing state preserved", store.Count())
	}
}
