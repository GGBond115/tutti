package relaytransport

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestExponentialBackoffUsesBoundedFullJitter(t *testing.T) {
	const seed = int64(73)
	backoff := newExponentialBackoff(BackoffConfig{
		Initial:     100 * time.Millisecond,
		Max:         time.Second,
		Multiplier:  2,
		RandFactory: func() *rand.Rand { return rand.New(rand.NewSource(seed)) },
	})

	expectedRandom := rand.New(rand.NewSource(seed))
	caps := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, time.Second}
	want := make([]time.Duration, len(caps))
	for i, cap := range caps {
		want[i] = time.Duration(expectedRandom.Int63n(int64(cap) + 1))
	}
	for i, expected := range want {
		if got := backoff.Next(); got != expected {
			t.Fatalf("Next() call %d = %s, want %s", i+1, got, expected)
		}
	}
}

func TestExponentialBackoffResetRestartsGeneration(t *testing.T) {
	const seed = int64(19)
	backoff := newExponentialBackoff(BackoffConfig{
		Initial:     100 * time.Millisecond,
		Max:         time.Second,
		Multiplier:  2,
		RandFactory: func() *rand.Rand { return rand.New(rand.NewSource(seed)) },
	})

	first := backoff.Next()
	second := backoff.Next()
	backoff.Reset()
	third := backoff.Next()

	expectedRandom := rand.New(rand.NewSource(seed))
	want := []time.Duration{
		time.Duration(expectedRandom.Int63n(int64(100*time.Millisecond) + 1)),
		time.Duration(expectedRandom.Int63n(int64(200*time.Millisecond) + 1)),
		time.Duration(expectedRandom.Int63n(int64(100*time.Millisecond) + 1)),
	}
	got := []time.Duration{first, second, third}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("delay %d = %s, want %s", i+1, got[i], want[i])
		}
	}
}

func TestExponentialBackoffSaturatesWithoutOverflow(t *testing.T) {
	const seed = int64(31)
	backoff := newExponentialBackoff(BackoffConfig{
		Initial:     time.Duration(math.MaxInt64 - 1),
		Max:         time.Duration(math.MaxInt64),
		Multiplier:  2,
		RandFactory: func() *rand.Rand { return rand.New(rand.NewSource(seed)) },
	})

	first := backoff.Next()
	second := backoff.Next()
	expectedRandom := rand.New(rand.NewSource(seed))
	if want := time.Duration(expectedRandom.Int63n(math.MaxInt64)); first != want {
		t.Fatalf("first delay = %s, want %s", first, want)
	}
	if want := time.Duration(expectedRandom.Int63()); second != want {
		t.Fatalf("second delay = %s, want %s", second, want)
	}
}
