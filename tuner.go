package main

import "math"

// Guitar tunings, low string to high.
var tunings = map[string][]struct {
	Name string
	Freq float64
}{
	"standard": {{"E2", 82.41}, {"A2", 110.00}, {"D3", 146.83}, {"G3", 196.00}, {"B3", 246.94}, {"E4", 329.63}},
	"dropd":    {{"D2", 73.42}, {"A2", 110.00}, {"D3", 146.83}, {"G3", 196.00}, {"B3", 246.94}, {"E4", 329.63}},
	"halfstep": {{"Eb2", 77.78}, {"Ab2", 103.83}, {"Db3", 138.59}, {"Gb3", 185.00}, {"Bb3", 233.08}, {"Eb4", 311.13}},
}

// Reading is what the UI shows for the tuner.
type Reading struct {
	Active bool    `json:"active"`
	Freq   float64 `json:"freq"`
	String int     `json:"string"` // 1 = lowest string
	Note   string  `json:"note"`
	Target float64 `json:"target"`
	Cents  float64 `json:"cents"`
}

// nearestString snaps a frequency to the closest string of a tuning.
func nearestString(freq float64, tuning string) Reading {
	strs, ok := tunings[tuning]
	if !ok {
		strs = tunings["standard"]
	}
	best, bestCents := 0, math.MaxFloat64
	for i, s := range strs {
		c := 1200 * math.Log2(freq/s.Freq)
		if math.Abs(c) < math.Abs(bestCents) {
			best, bestCents = i, c
		}
	}
	return Reading{Active: true, Freq: freq, String: best + 1, Note: strs[best].Name, Target: strs[best].Freq, Cents: bestCents}
}

const (
	yinWindow = 2048
	yinMaxTau = rate / 60  // lowest detectable ~60 Hz
	yinMinTau = rate / 500 // highest detectable ~500 Hz
	// samples the detector needs
	yinNeeded = yinWindow + yinMaxTau + 1
)

// detectPitch runs the YIN algorithm on the tail of buf. ok is false when the
// signal is too quiet or not clearly pitched.
func detectPitch(buf []float64) (freq float64, ok bool) {
	if len(buf) < yinNeeded {
		return 0, false
	}
	buf = buf[len(buf)-yinNeeded:]

	var sq float64
	for _, v := range buf[:yinWindow] {
		sq += v * v
	}
	if math.Sqrt(sq/yinWindow) < 0.003 {
		return 0, false
	}

	// Difference function, then cumulative mean normalisation.
	cm := make([]float64, yinMaxTau+1)
	cm[0] = 1
	var run float64
	for tau := 1; tau <= yinMaxTau; tau++ {
		var d float64
		for i := 0; i < yinWindow; i++ {
			x := buf[i] - buf[i+tau]
			d += x * x
		}
		run += d
		if run == 0 {
			cm[tau] = 1
		} else {
			cm[tau] = d * float64(tau) / run
		}
	}

	// First dip below threshold, followed down to its local minimum.
	const threshold = 0.15
	for tau := yinMinTau; tau < yinMaxTau; tau++ {
		if cm[tau] < threshold {
			for tau+1 < yinMaxTau && cm[tau+1] < cm[tau] {
				tau++
			}
			// Parabolic interpolation for sub-sample accuracy.
			t := float64(tau)
			a, b, c := cm[tau-1], cm[tau], cm[tau+1]
			if den := a - 2*b + c; den != 0 {
				t += (a - c) / (2 * den)
			}
			return rate / t, true
		}
	}
	return 0, false
}
