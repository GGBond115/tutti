package relaytransport

import (
	"math"
	"math/rand"
	"time"
)

type exponentialBackoff struct {
	cfg     BackoffConfig
	current time.Duration
	random  *rand.Rand
}

func newExponentialBackoff(cfg BackoffConfig) *exponentialBackoff {
	if cfg.Initial <= 0 {
		cfg.Initial = 100 * time.Millisecond
	}
	if cfg.Max <= 0 {
		cfg.Max = 5 * time.Second
	}
	if cfg.Multiplier <= 1 || math.IsNaN(cfg.Multiplier) || math.IsInf(cfg.Multiplier, 0) {
		cfg.Multiplier = 2
	}
	if cfg.Initial > cfg.Max {
		cfg.Initial = cfg.Max
	}
	var random *rand.Rand
	if cfg.RandFactory != nil {
		random = cfg.RandFactory()
	}
	if random == nil {
		// The package-level generator is concurrency-safe and supplies distinct
		// seeds to the generator owned by each business lifecycle.
		random = rand.New(rand.NewSource(rand.Int63()))
	}
	return &exponentialBackoff{cfg: cfg, random: random}
}

func (b *exponentialBackoff) Reset() { b.current = 0 }

func (b *exponentialBackoff) Cap() time.Duration { return b.current }

func (b *exponentialBackoff) Next() time.Duration {
	if b.current == 0 {
		b.current = b.cfg.Initial
	} else if float64(b.current) >= float64(b.cfg.Max)/b.cfg.Multiplier {
		b.current = b.cfg.Max
	} else {
		next := time.Duration(float64(b.current) * b.cfg.Multiplier)
		if next <= b.current || next > b.cfg.Max {
			b.current = b.cfg.Max
		} else {
			b.current = next
		}
	}
	if b.current <= 0 {
		return 0
	}
	if b.current == time.Duration(math.MaxInt64) {
		return time.Duration(b.random.Int63())
	}
	return time.Duration(b.random.Int63n(int64(b.current) + 1))
}
