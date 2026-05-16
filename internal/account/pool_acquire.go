package account

import (
	"context"

	"ds2api/internal/config"
)

func (p *Pool) Acquire(target string, exclude map[string]bool) (config.Account, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.acquireLocked(target, normalizeExclude(exclude))
}

func (p *Pool) AcquireWait(ctx context.Context, target string, exclude map[string]bool) (config.Account, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	exclude = normalizeExclude(exclude)
	for {
		if ctx.Err() != nil {
			return config.Account{}, false
		}

		p.mu.Lock()
		if acc, ok := p.acquireLocked(target, exclude); ok {
			p.mu.Unlock()
			return acc, true
		}
		if !p.canQueueLocked(target, exclude) {
			p.mu.Unlock()
			return config.Account{}, false
		}
		waiter := make(chan struct{})
		p.waiters = append(p.waiters, waiter)
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.removeWaiterLocked(waiter)
			p.mu.Unlock()
			return config.Account{}, false
		case <-waiter:
		}
	}
}

func (p *Pool) acquireLocked(target string, exclude map[string]bool) (config.Account, bool) {
	if target != "" {
		if exclude[target] || !p.canAcquireIDLocked(target) {
			return config.Account{}, false
		}
		acc, ok := p.store.FindAccount(target)
		if !ok {
			return config.Account{}, false
		}
		p.inUse[target]++
		p.bumpQueue(target)
		return acc, true
	}

	return p.tryAcquire(exclude)
}

func (p *Pool) tryAcquire(exclude map[string]bool) (config.Account, bool) {
	// Build list of eligible candidates (not excluded, not at capacity, not disabled).
	candidates := make([]string, 0, len(p.queue))
	for _, id := range p.queue {
		if exclude[id] || !p.canAcquireIDLocked(id) {
			continue
		}
		if p.weights != nil && p.weights.IsDisabled(id) {
			continue
		}
		candidates = append(candidates, id)
	}

	if len(candidates) == 0 {
		return config.Account{}, false
	}

	// Use weighted random selection if weights are available.
	var pickedID string
	var ok bool
	if p.weights != nil {
		pickedID, ok = p.weights.WeightedPick(candidates)
	} else {
		// Fallback: pick first candidate (same as original logic).
		pickedID = candidates[0]
		ok = true
	}
	if !ok {
		return config.Account{}, false
	}

	acc, found := p.store.FindAccount(pickedID)
	if !found {
		return config.Account{}, false
	}
	p.inUse[pickedID]++
	p.bumpQueue(pickedID)
	return acc, true
}

func (p *Pool) bumpQueue(accountID string) {
	for i, id := range p.queue {
		if id != accountID {
			continue
		}
		p.queue = append(p.queue[:i], p.queue[i+1:]...)
		p.queue = append(p.queue, accountID)
		return
	}
}

func normalizeExclude(exclude map[string]bool) map[string]bool {
	if exclude == nil {
		return map[string]bool{}
	}
	return exclude
}
