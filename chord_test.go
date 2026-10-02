package main

import (
	"math"
	"math/rand"
	"testing"
)

var openStrings = [6]float64{82.41, 110.00, 146.83, 196.00, 246.94, 329.63}

// strum synthesises a chord from fret numbers (-1 = muted) as plucked strings: a
// fundamental plus decaying harmonics, with random phases, level and a little noise.
func strum(frets [6]int, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	buf := make([]float64, chordWindow)
	for s, fret := range frets {
		if fret < 0 {
			continue
		}
		f := openStrings[s] * math.Pow(2, float64(fret)/12)
		amp, ph0 := 0.5+0.5*rng.Float64(), rng.Float64()*2*math.Pi
		for k := 1; k <= 8; k++ {
			a := amp * 0.1 / float64(k)
			ph := ph0 + float64(k)*rng.Float64()
			for i := range buf {
				buf[i] += a * math.Sin(2*math.Pi*f*float64(k)*float64(i)/rate+ph)
			}
		}
	}
	for i := range buf {
		buf[i] += 0.002 * rng.NormFloat64()
	}
	return buf
}

func TestDetectChord(t *testing.T) {
	cases := []struct {
		name  string
		frets [6]int
	}{
		{"C", [6]int{-1, 3, 2, 0, 1, 0}}, {"G", [6]int{3, 2, 0, 0, 0, 3}}, {"D", [6]int{-1, -1, 0, 2, 3, 2}},
		{"A", [6]int{-1, 0, 2, 2, 2, 0}}, {"E", [6]int{0, 2, 2, 1, 0, 0}}, {"F", [6]int{1, 3, 3, 2, 1, 1}},
		{"Am", [6]int{-1, 0, 2, 2, 1, 0}}, {"Em", [6]int{0, 2, 2, 0, 0, 0}}, {"Dm", [6]int{-1, -1, 0, 2, 3, 1}},
		{"Bm", [6]int{-1, 2, 4, 4, 3, 2}}, {"A#", [6]int{-1, 1, 3, 3, 3, 1}}, {"F#m", [6]int{2, 4, 4, 2, 2, 2}},
		{"E7", [6]int{0, 2, 0, 1, 0, 0}}, {"G7", [6]int{3, 2, 0, 0, 0, 1}}, {"A7", [6]int{-1, 0, 2, 0, 2, 0}},
		{"Cmaj7", [6]int{-1, 3, 2, 0, 0, 0}}, {"Am7", [6]int{-1, 0, 2, 0, 1, 0}}, {"Em7", [6]int{0, 2, 2, 0, 3, 0}},
		{"Dsus4", [6]int{-1, -1, 0, 2, 3, 3}}, {"Asus2", [6]int{-1, 0, 2, 2, 0, 0}},
	}
	wrong := 0
	for _, c := range cases {
		for seed := int64(1); seed <= 6; seed++ {
			got := detectChord(strum(c.frets, seed))
			if !got.Active || got.Name != c.name {
				wrong++
				t.Errorf("%s (seed %d): got %q active=%v score=%.2f", c.name, seed, got.Name, got.Active, got.Score)
			}
		}
	}
	t.Logf("%d/%d wrong", wrong, len(cases)*6)
}

func TestDetectChordRejects(t *testing.T) {
	if detectChord(make([]float64, chordWindow)).Active {
		t.Error("silence detected as a chord")
	}
	if detectChord(strum([6]int{-1, 3, -1, -1, -1, -1}, 1)).Active {
		t.Error("single note detected as a chord")
	}
	rng := rand.New(rand.NewSource(1))
	noise := make([]float64, chordWindow)
	for i := range noise {
		noise[i] = 0.05 * rng.NormFloat64()
	}
	if detectChord(noise).Active {
		t.Error("noise detected as a chord")
	}
}
