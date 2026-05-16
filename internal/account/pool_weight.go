package account

import (
	"math/rand"
	"sort"
	"sync"
	"time"

	"ds2api/internal/config"
)

const (
	// DefaultWeight is the starting/maximum weight for any account.
	DefaultWeight = 100
	// WeightDegradeFactor is the fraction of maxWeight subtracted per failure.
	// Each failure: weight -= maxWeight * WeightDegradeFactor (i.e. -20 per failure).
	WeightDegradeFactor = 0.2
	// ConsecutiveSuccessesToRecover is how many sequential successes reset weight.
	ConsecutiveSuccessesToRecover = 5
	// RecoveryGracePeriod is how long after the last failure weight is auto-restored.
	RecoveryGracePeriod = 30 * time.Minute
	// WeightCheckInterval is how often the background recovery checker runs.
	WeightCheckInterval = 1 * time.Minute
)

type accountWeightState struct {
	maxWeight          int
	currentWeight      int
	consecutiveFails   int
	consecutiveSuccesses int
	lastFailureAt      time.Time
	lastSuccessAt      time.Time
	disabled           bool // true when weight hits 0, requires admin re-enable
}

// weights manages runtime account weight state.
type weights struct {
	mu     sync.Mutex
	states map[string]*accountWeightState
	stopCh chan struct{}
}

func newWeights() *weights {
	w := &weights{
		states: make(map[string]*accountWeightState),
		stopCh: make(chan struct{}),
	}
	go w.recoveryLoop()
	return w
}

func (w *weights) stop() {
	close(w.stopCh)
}

// initAccount creates a fresh weight state for an account.
func (w *weights) initAccount(accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.states[accountID] = &accountWeightState{
		maxWeight:          DefaultWeight,
		currentWeight:      DefaultWeight,
		consecutiveFails:   0,
		consecutiveSuccesses: 0,
		disabled:           false,
	}
}

// removeAccount removes weight tracking for an account (e.g. account deleted).
func (w *weights) removeAccount(accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.states, accountID)
}

// resetAll clears all weights and re-initializes for the given account IDs.
func (w *weights) resetAll(accountIDs []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.states = make(map[string]*accountWeightState, len(accountIDs))
	for _, id := range accountIDs {
		w.states[id] = &accountWeightState{
			maxWeight:          DefaultWeight,
			currentWeight:      DefaultWeight,
			consecutiveFails:   0,
			consecutiveSuccesses: 0,
			disabled:           false,
		}
	}
}

// ReportSuccess records a successful request for an account.
// If consecutive successes reach threshold, weight is restored fully.
func (w *weights) ReportSuccess(accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, ok := w.states[accountID]
	if !ok || state.disabled {
		return
	}

	state.lastSuccessAt = time.Now()
	state.consecutiveSuccesses++
	state.consecutiveFails = 0

	// Restore weight after enough consecutive successes.
	if state.consecutiveSuccesses >= ConsecutiveSuccessesToRecover && state.currentWeight < state.maxWeight {
		config.Logger.Info("[weight] account recovered by consecutive successes",
			"account", accountID,
			"old_weight", state.currentWeight,
			"new_weight", state.maxWeight,
			"consecutive_successes", state.consecutiveSuccesses,
		)
		state.currentWeight = state.maxWeight
		state.consecutiveSuccesses = 0
	}
}

// ReportFailure records a failed request for an account.
// Reduces weight by WeightDegradeFactor * maxWeight. If weight hits 0, account is disabled.
func (w *weights) ReportFailure(accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, ok := w.states[accountID]
	if !ok || state.disabled {
		return
	}

	state.lastFailureAt = time.Now()
	state.consecutiveFails++
	state.consecutiveSuccesses = 0

	degradation := int(float64(state.maxWeight) * WeightDegradeFactor)
	state.currentWeight -= degradation
	if state.currentWeight <= 0 {
		state.currentWeight = 0
		state.disabled = true
		config.Logger.Warn("[weight] account disabled due to repeated failures",
			"account", accountID,
			"consecutive_fails", state.consecutiveFails,
		)
		return
	}

	config.Logger.Warn("[weight] account degraded",
		"account", accountID,
		"weight", state.currentWeight,
		"max_weight", state.maxWeight,
		"consecutive_fails", state.consecutiveFails,
	)
}

// Reenable manually re-enables a disabled account, restoring its weight.
func (w *weights) Reenable(accountID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, ok := w.states[accountID]
	if !ok {
		return false
	}
	if !state.disabled {
		return true // already enabled
	}

	state.disabled = false
	state.currentWeight = state.maxWeight
	state.consecutiveFails = 0
	state.consecutiveSuccesses = 0
	config.Logger.Info("[weight] account re-enabled by admin",
		"account", accountID,
		"weight", state.maxWeight,
	)
	return true
}

// IsDisabled returns true if the account is disabled (weight=0).
func (w *weights) IsDisabled(accountID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, ok := w.states[accountID]
	return ok && state.disabled
}

// GetCurrentWeight returns the current weight for an account.
func (w *weights) GetCurrentWeight(accountID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	state, ok := w.states[accountID]
	if !ok {
		return DefaultWeight
	}
	if state.disabled {
		return 0
	}
	return state.currentWeight
}

// AllEnabledIDs returns all account IDs that are NOT disabled.
func (w *weights) AllEnabledIDs(accountIDs []string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]string, 0, len(accountIDs))
	for _, id := range accountIDs {
		state, ok := w.states[id]
		if !ok || (ok && state.disabled) {
			continue
		}
		if ok && state.currentWeight <= 0 {
			continue
		}
		result = append(result, id)
	}
	return result
}

// WeightedPick picks an account ID from candidates.
// When all candidates have equal weight, picks the first one (maintains queue order / round-robin).
// When weights differ, uses weighted random selection so higher-weight accounts are picked more often.
// Returns the picked ID and whether it's found.
func (w *weights) WeightedPick(candidates []string) (string, bool) {
	if len(candidates) == 0 {
		return "", false
	}

	w.mu.Lock()
	// Build weighted list.
	type candidate struct {
		id     string
		weight int
	}
	pool := make([]candidate, 0, len(candidates))
	allEqual := true
	var firstWeight int
	totalWeight := 0
	for i, id := range candidates {
		state, ok := w.states[id]
		if !ok || state.disabled || state.currentWeight <= 0 {
			continue
		}
		pool = append(pool, candidate{id: id, weight: state.currentWeight})
		totalWeight += state.currentWeight
		if i == 0 {
			firstWeight = state.currentWeight
		} else if state.currentWeight != firstWeight {
			allEqual = false
		}
	}
	w.mu.Unlock()

	if len(pool) == 0 || totalWeight <= 0 {
		return "", false
	}

	// When all weights are equal, pick the first candidate (preserves queue order / round-robin).
	if allEqual {
		return pool[0].id, true
	}

	// Weighted random selection.
	r := rand.Intn(totalWeight)
	cumulative := 0
	for _, c := range pool {
		cumulative += c.weight
		if r < cumulative {
			return c.id, true
		}
	}

	// Fallback: return last candidate.
	return pool[len(pool)-1].id, true
}

// GetWeightStatus returns a snapshot of all accounts' weight states.
func (w *weights) GetWeightStatus() []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Sort for deterministic output.
	ids := make([]string, 0, len(w.states))
	for id := range w.states {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	result := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		state := w.states[id]
		result = append(result, map[string]any{
			"account_id":           id,
			"max_weight":           state.maxWeight,
			"current_weight":       state.currentWeight,
			"disabled":             state.disabled,
			"consecutive_fails":    state.consecutiveFails,
			"consecutive_successes": state.consecutiveSuccesses,
			"last_failure_at":      formatTimeOrNil(state.lastFailureAt),
			"last_success_at":      formatTimeOrNil(state.lastSuccessAt),
		})
	}
	return result
}

// checkRecovery checks if any degraded account should be restored via time-based recovery.
func (w *weights) checkRecovery() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	for id, state := range w.states {
		if state.disabled || state.currentWeight >= state.maxWeight {
			continue
		}
		if state.lastFailureAt.IsZero() {
			continue
		}
		if now.Sub(state.lastFailureAt) >= RecoveryGracePeriod {
			config.Logger.Info("[weight] account recovered by time grace period",
				"account", id,
				"old_weight", state.currentWeight,
				"new_weight", state.maxWeight,
				"minutes_since_last_failure", now.Sub(state.lastFailureAt).Minutes(),
			)
			state.currentWeight = state.maxWeight
			state.consecutiveFails = 0
			state.consecutiveSuccesses = 0
		}
	}
}

// recoveryLoop runs periodically to check for time-based weight recovery.
func (w *weights) recoveryLoop() {
	ticker := time.NewTicker(WeightCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.checkRecovery()
		case <-w.stopCh:
			return
		}
	}
}

func formatTimeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}
