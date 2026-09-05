package diagnosis

import (
	"sync"
	"time"
)

type Record struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Dimension string `json:"dimension"`
	Score     int    `json:"score"`
	Question  string `json:"question"`
	SessionID string `json:"sessionId"`
	UserID    string `json:"userId"`
}

type SM2State struct {
	Dimension   string
	EaseFactor  float64
	Interval    int
	Repetitions int
	NextReview  int64
}

type Store struct {
	mu      sync.RWMutex
	records []Record
	states  map[string]SM2State
}

func NewStore() *Store {
	return &Store{states: make(map[string]SM2State)}
}

func (s *Store) Add(record Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	s.applySM2(Scope(record.UserID, record.SessionID), record.Dimension, record.Score)
}

func (s *Store) Load(records []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = nil
	s.states = make(map[string]SM2State)
	for _, record := range records {
		s.records = append(s.records, record)
		s.applySM2(Scope(record.UserID, record.SessionID), record.Dimension, record.Score)
	}
}

func (s *Store) Snapshot(userID, sessionID string) ([]Record, map[string]SM2State) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, 0)
	for _, record := range s.records {
		if userID != "" && record.UserID == userID {
			records = append(records, record)
		} else if userID == "" && sessionID != "" && record.UserID == "" && record.SessionID == sessionID {
			records = append(records, record)
		}
	}

	scope := Scope(userID, sessionID)
	states := make(map[string]SM2State)
	for key, state := range s.states {
		if key == SM2Key(scope, state.Dimension) {
			states[state.Dimension] = state
		}
	}
	return records, states
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.records)
}

func Scope(userID, sessionID string) string {
	if userID != "" {
		return "user:" + userID
	}
	return "session:" + sessionID
}

func SM2Key(scope, dimension string) string {
	return scope + "\x00" + dimension
}

// applySM2 updates spaced-repetition state. The caller must hold s.mu.
func (s *Store) applySM2(scope, dimension string, score int) {
	key := SM2Key(scope, dimension)
	existing, ok := s.states[key]
	if !ok {
		existing = SM2State{
			Dimension:   dimension,
			EaseFactor:  2.5,
			Interval:    1,
			Repetitions: 0,
			NextReview:  time.Now().UnixMilli(),
		}
	}

	quality := max(0, min(5, score*5/10))
	interval := 1
	if quality >= 3 {
		switch existing.Repetitions {
		case 0:
			interval = 1
		case 1:
			interval = 3
		default:
			interval = int(float64(existing.Interval) * existing.EaseFactor)
		}
		existing.Repetitions++
	} else {
		existing.Repetitions = 0
	}

	existing.EaseFactor = max(1.3, existing.EaseFactor+(0.1-float64(5-quality)*(0.08+float64(5-quality)*0.02)))
	existing.Interval = interval
	existing.NextReview = time.Now().UnixMilli() + int64(interval)*24*60*60*1000
	s.states[key] = existing
}
