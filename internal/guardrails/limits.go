package guardrails

import (
	"sync"
	"time"
)

// Limiter is a sliding-window rate limiter keyed by an arbitrary string
// (here, a hashed client address). It is in-memory and per-process, which is
// the right size for a single-instance demo; a multi-instance deployment
// would move the counters to Postgres or Redis behind the same interface.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
	now    func() time.Time
}

// NewLimiter allows limit events per window for each key.
func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

// Allow records an event for key if it is within the limit. When it is not,
// it returns how long until the next event would be allowed.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil || l.limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false, kept[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(kept, now)
	// Opportunistic cleanup so abandoned keys do not accumulate.
	if len(l.hits) > 10_000 {
		for k, ts := range l.hits {
			if len(ts) == 0 || !ts[len(ts)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return true, 0
}

// Remaining reports how many events key may still make in the current window.
func (l *Limiter) Remaining(key string) int {
	if l == nil || l.limit <= 0 {
		return -1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-l.window)
	n := 0
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			n++
		}
	}
	return max(l.limit-n, 0)
}

// Budget is a daily spending cap in US dollars across all visitors. When it
// is spent the demo stops calling the model until the next UTC day. This is
// the control that bounds the bill no matter what else fails.
type Budget struct {
	mu    sync.Mutex
	limit float64
	spent float64
	day   string
	now   func() time.Time
}

// NewBudget returns a budget with the given daily limit. A limit of zero
// disables the cap.
func NewBudget(limitUSD float64) *Budget {
	b := &Budget{limit: limitUSD, now: time.Now}
	b.day = b.today()
	return b
}

func (b *Budget) today() string { return b.now().UTC().Format("2006-01-02") }

func (b *Budget) rollLocked() {
	if d := b.today(); d != b.day {
		b.day, b.spent = d, 0
	}
}

// Seed sets today's spend, used at startup to resume from the database.
func (b *Budget) Seed(spentUSD float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	b.spent = spentUSD
}

// Add records spend.
func (b *Budget) Add(usd float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	b.spent += usd
}

// Exhausted reports whether today's limit has been reached.
func (b *Budget) Exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.limit > 0 && b.spent >= b.limit
}

// Status returns today's spend and the limit.
func (b *Budget) Status() (spent, limit float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rollLocked()
	return b.spent, b.limit
}
