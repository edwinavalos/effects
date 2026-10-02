//go:build ignore

// Parked: the animal-sounds pedal concept, to be reworked later. Not part of the build.

package main

import (
	"math"
	"testing"
)

func TestAnimalsRender(t *testing.T) {
	for _, name := range []string{"dog", "cat", "cow"} {
		// natural pitch, and transposed up and down to follow guitar notes
		for _, scale := range []float64{1, 0.4, 3} {
			s := newSynth(name, scale)
			var peak float64
			n := 0
			for {
				y, ok := s.next(scale)
				if !ok {
					break
				}
				if math.IsNaN(y) || math.IsInf(y, 0) {
					t.Fatalf("%s x%.1f: NaN/Inf at sample %d", name, scale, n)
				}
				peak = math.Max(peak, math.Abs(y))
				n++
			}
			if n < rate/2 || peak < 0.2 || peak > 1.5 {
				t.Errorf("%s x%.1f: %d samples, peak %v", name, scale, n, peak)
			}
		}
	}
}
