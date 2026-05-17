package account

import (
	"sort"
	"sync"

	"ds2api/internal/config"
)

type Pool struct {
	store                  *config.Store
	mu                     sync.Mutex
	queue                  []string
	inUse                  map[string]int
	waiters                []chan struct{}
	maxInflightPerAccount  int
	recommendedConcurrency int
	maxQueueSize           int
	globalMaxInflight      int
	weights                *weights
	stats                  *stats
}

func NewPool(store *config.Store) *Pool {
	maxPer := 2
	if store != nil {
		maxPer = store.RuntimeAccountMaxInflight()
	}
	p := &Pool{
		store:                 store,
		inUse:                 map[string]int{},
		maxInflightPerAccount: maxPer,
		weights:               newWeights(),
		stats:                 newStats(),
	}
	p.Reset()
	return p
}

func (p *Pool) Reset() {
	accounts := p.store.Accounts()
	sort.SliceStable(accounts, func(i, j int) bool {
		iHas := accounts[i].Token != ""
		jHas := accounts[j].Token != ""
		if iHas == jHas {
			return i < j
		}
		return iHas
	})
	ids := make([]string, 0, len(accounts))
	for _, a := range accounts {
		id := a.Identifier()
		if id != "" {
			ids = append(ids, id)
		}
	}
	if p.store != nil {
		p.maxInflightPerAccount = p.store.RuntimeAccountMaxInflight()
	} else {
		p.maxInflightPerAccount = maxInflightFromEnv()
	}
	recommended := defaultRecommendedConcurrency(len(ids), p.maxInflightPerAccount)
	queueLimit := maxQueueFromEnv(recommended)
	globalLimit := recommended
	if p.store != nil {
		queueLimit = p.store.RuntimeAccountMaxQueue(recommended)
		globalLimit = p.store.RuntimeGlobalMaxInflight(recommended)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.drainWaitersLocked()
	p.queue = ids
	p.inUse = map[string]int{}
	p.recommendedConcurrency = recommended
	p.maxQueueSize = queueLimit
	p.globalMaxInflight = globalLimit
	p.weights.resetAll(ids)
	config.Logger.Info(
		"[init_account_queue] initialized",
		"total", len(ids),
		"max_inflight_per_account", p.maxInflightPerAccount,
		"global_max_inflight", p.globalMaxInflight,
		"recommended_concurrency", p.recommendedConcurrency,
		"max_queue_size", p.maxQueueSize,
	)
}

func (p *Pool) Release(accountID string) {
	if accountID == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	count := p.inUse[accountID]
	if count <= 0 {
		return
	}
	if count == 1 {
		delete(p.inUse, accountID)
		p.notifyWaiterLocked()
		return
	}
	p.inUse[accountID] = count - 1
	p.notifyWaiterLocked()
}

func (p *Pool) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	available := make([]string, 0, len(p.queue))
	inUseAccounts := make([]string, 0, len(p.inUse))
	inUseSlots := 0
	for _, id := range p.queue {
		if p.inUse[id] < p.maxInflightPerAccount {
			available = append(available, id)
		}
	}
	for id, count := range p.inUse {
		if count > 0 {
			inUseAccounts = append(inUseAccounts, id)
			inUseSlots += count
		}
	}
	sort.Strings(inUseAccounts)
	return map[string]any{
		"available":                len(available),
		"in_use":                   inUseSlots,
		"total":                    len(p.store.Accounts()),
		"available_accounts":       available,
		"in_use_accounts":          inUseAccounts,
		"max_inflight_per_account": p.maxInflightPerAccount,
		"global_max_inflight":      p.globalMaxInflight,
		"recommended_concurrency":  p.recommendedConcurrency,
		"waiting":                  len(p.waiters),
		"max_queue_size":           p.maxQueueSize,
	}
}

// ReportAccountSuccess records a successful request for the weighted account selection.
func (p *Pool) ReportAccountSuccess(accountID string) {
	if p.weights != nil {
		p.weights.ReportSuccess(accountID)
	}
}

// ReportAccountFailure records a failed request for the weighted account selection.
func (p *Pool) ReportAccountFailure(accountID string) {
	if p.weights != nil {
		p.weights.ReportFailure(accountID)
	}
}

// ReenableAccount manually re-enables a disabled account.
func (p *Pool) ReenableAccount(accountID string) bool {
	if p.weights == nil {
		return false
	}
	return p.weights.Reenable(accountID)
}

// DisableAccount manually disables an account so the smart router skips it
// until it is re-enabled by an admin.
func (p *Pool) DisableAccount(accountID string) bool {
	if p.weights == nil {
		return false
	}
	return p.weights.Disable(accountID)
}

// WeightStatus returns weight state for all accounts.
func (p *Pool) WeightStatus() []map[string]any {
	if p.weights == nil {
		return nil
	}
	return p.weights.GetWeightStatus()
}

// GetWeights returns the weights instance (for acquireLocked access).
func (p *Pool) GetWeights() *weights {
	return p.weights
}

// RecordRequestSuccess records a successful request including token usage,
// keyed by both account ID and API key. Either id may be empty; empty ids are skipped.
func (p *Pool) RecordRequestSuccess(accountID, apiKey string, inputTokens, outputTokens int) {
	if p.stats != nil {
		p.stats.RecordSuccess(accountID, apiKey, inputTokens, outputTokens)
	}
}

// RecordRequestFailure records a failed request keyed by both account ID and API key.
func (p *Pool) RecordRequestFailure(accountID, apiKey string) {
	if p.stats != nil {
		p.stats.RecordFailure(accountID, apiKey)
	}
}

// AccountStatsSnapshot returns per-account request statistics.
func (p *Pool) AccountStatsSnapshot() []map[string]any {
	if p.stats == nil {
		return nil
	}
	return p.stats.AccountsSnapshot()
}

// APIKeyStatsSnapshot returns per-api-key request statistics.
func (p *Pool) APIKeyStatsSnapshot() []map[string]any {
	if p.stats == nil {
		return nil
	}
	return p.stats.KeysSnapshot()
}

// ForgetAccountStats removes stats for the given account id (e.g. account deleted).
func (p *Pool) ForgetAccountStats(accountID string) {
	if p.stats != nil {
		p.stats.removeAccount(accountID)
	}
}

// ForgetAPIKeyStats removes stats for the given api key value (e.g. key deleted).
func (p *Pool) ForgetAPIKeyStats(apiKey string) {
	if p.stats != nil {
		p.stats.removeKey(apiKey)
	}
}

// ResetStats wipes all per-account / per-key counters.
func (p *Pool) ResetStats() {
	if p.stats != nil {
		p.stats.resetAll()
	}
}
