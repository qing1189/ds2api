package account

import (
	"sort"
	"sync"
	"time"
)

// StatsEntry is a per-key (account or api key) counter snapshot.
type StatsEntry struct {
	mu            sync.Mutex
	Total         int64
	Success       int64
	Failure       int64
	InputTokens   int64
	OutputTokens  int64
	LastSuccessAt time.Time
	LastFailureAt time.Time
	LastRequestAt time.Time
}

func (e *StatsEntry) addSuccess(now time.Time, input, output int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Total++
	e.Success++
	if input > 0 {
		e.InputTokens += int64(input)
	}
	if output > 0 {
		e.OutputTokens += int64(output)
	}
	e.LastSuccessAt = now
	e.LastRequestAt = now
}

func (e *StatsEntry) addFailure(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Total++
	e.Failure++
	e.LastFailureAt = now
	e.LastRequestAt = now
}

// stats collects per-account and per-api-key request counters.
// All operations are safe for concurrent use.
type stats struct {
	mu       sync.RWMutex
	accounts map[string]*StatsEntry
	keys     map[string]*StatsEntry
}

func newStats() *stats {
	return &stats{
		accounts: make(map[string]*StatsEntry),
		keys:     make(map[string]*StatsEntry),
	}
}

func (s *stats) entryAccount(id string) *StatsEntry {
	if id == "" {
		return nil
	}
	s.mu.RLock()
	e, ok := s.accounts[id]
	s.mu.RUnlock()
	if ok {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok = s.accounts[id]
	if !ok {
		e = &StatsEntry{}
		s.accounts[id] = e
	}
	return e
}

func (s *stats) entryKey(id string) *StatsEntry {
	if id == "" {
		return nil
	}
	s.mu.RLock()
	e, ok := s.keys[id]
	s.mu.RUnlock()
	if ok {
		return e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok = s.keys[id]
	if !ok {
		e = &StatsEntry{}
		s.keys[id] = e
	}
	return e
}

// RecordSuccess increments counters for both account and api key on success.
func (s *stats) RecordSuccess(accountID, apiKey string, input, output int) {
	now := time.Now()
	if accountID != "" {
		s.entryAccount(accountID).addSuccess(now, input, output)
	}
	if apiKey != "" {
		s.entryKey(apiKey).addSuccess(now, input, output)
	}
}

// RecordFailure increments failure counters for both account and api key.
func (s *stats) RecordFailure(accountID, apiKey string) {
	now := time.Now()
	if accountID != "" {
		s.entryAccount(accountID).addFailure(now)
	}
	if apiKey != "" {
		s.entryKey(apiKey).addFailure(now)
	}
}

// removeAccount drops stats for the given account id (called when account is deleted).
func (s *stats) removeAccount(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accounts, id)
}

// removeKey drops stats for the given api key value (called when key is deleted).
func (s *stats) removeKey(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, id)
}

// resetAll wipes all in-memory counters.
func (s *stats) resetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts = make(map[string]*StatsEntry)
	s.keys = make(map[string]*StatsEntry)
}

func snapshotEntry(e *StatsEntry) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]any{
		"total_requests":  e.Total,
		"success_count":   e.Success,
		"failure_count":   e.Failure,
		"input_tokens":    e.InputTokens,
		"output_tokens":   e.OutputTokens,
		"last_success_at": formatTimeOrNil(e.LastSuccessAt),
		"last_failure_at": formatTimeOrNil(e.LastFailureAt),
		"last_request_at": formatTimeOrNil(e.LastRequestAt),
	}
}

// AccountsSnapshot returns a deterministic list of per-account stats.
func (s *stats) AccountsSnapshot() []map[string]any {
	s.mu.RLock()
	ids := make([]string, 0, len(s.accounts))
	for id := range s.accounts {
		ids = append(ids, id)
	}
	entries := make(map[string]*StatsEntry, len(s.accounts))
	for k, v := range s.accounts {
		entries[k] = v
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row := snapshotEntry(entries[id])
		row["account_id"] = id
		out = append(out, row)
	}
	return out
}

// KeysSnapshot returns a deterministic list of per-api-key stats.
func (s *stats) KeysSnapshot() []map[string]any {
	s.mu.RLock()
	ids := make([]string, 0, len(s.keys))
	for id := range s.keys {
		ids = append(ids, id)
	}
	entries := make(map[string]*StatsEntry, len(s.keys))
	for k, v := range s.keys {
		entries[k] = v
	}
	s.mu.RUnlock()
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row := snapshotEntry(entries[id])
		row["api_key"] = id
		out = append(out, row)
	}
	return out
}
