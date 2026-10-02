package main

import (
	"math"
	"math/rand"
	"testing"
)

// strikes synthesises plucked chords starting at the given times (seconds): a short
// noisy attack, a few decaying partials, and a ring that overlaps the next strike.
func strikes(seconds float64, at, amp []float64) []float64 {
	rng := rand.New(rand.NewSource(7))
	buf := make([]float64, int(seconds*rate))
	for n, t0 := range at {
		start := int(t0 * rate)
		for i := start; i < len(buf); i++ {
			t := float64(i-start) / rate
			env := math.Min(1, t/0.002) * math.Exp(-t/0.35)
			var v float64
			for _, f := range []float64{110, 165, 220, 277, 330} {
				for k := 1; k <= 4; k++ {
					v += math.Sin(2*math.Pi*f*float64(k)*t) / float64(k)
				}
			}
			v = v*0.04 + 0.15*rng.NormFloat64()*math.Exp(-t/0.006)
			buf[i] += amp[n] * env * v
		}
	}
	return buf
}

func onsetTimes(buf []float64) []float64 {
	var d onsetDetector
	var out []float64
	for i := 0; i+blockSize <= len(buf); i += blockSize {
		if on, _ := d.process(buf[i : i+blockSize]); on {
			out = append(out, float64(i+blockSize)/rate) // time at the end of the block, as when it is read live
		}
	}
	return out
}

func TestOnsets(t *testing.T) {
	// Eighth notes at ~70 bpm: each strike lands on the previous one's ring, and up-strums are softer.
	at := []float64{0.3, 0.73, 1.16, 1.59, 2.02, 2.45}
	amp := []float64{1, 0.6, 0.9, 0.6, 1, 0.6}
	got := onsetTimes(strikes(3, at, amp))
	if len(got) != len(at) {
		t.Fatalf("want %d onsets, got %d: %v", len(at), len(got), got)
	}
	for i, g := range got {
		if g < at[i] || g > at[i]+0.012 {
			t.Errorf("onset %d at %.3f, strike at %.3f", i, g, at[i])
		}
	}
}

func TestOnsetsQuiet(t *testing.T) {
	if got := onsetTimes(make([]float64, rate)); len(got) != 0 {
		t.Errorf("silence gave onsets: %v", got)
	}
	// One strike, then only its decay: no further onsets.
	if got := onsetTimes(strikes(2, []float64{0.2}, []float64{1})); len(got) != 1 {
		t.Errorf("a single strike gave %d onsets", len(got))
	}
}
