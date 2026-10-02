package main

import (
	"math"
	"time"
)

const (
	onsetRefBlocks = 16                    // loudness history compared against (~43 ms)
	onsetCooldown  = 34                    // blocks to ignore after an onset (~90 ms): a strum's strings count as one
	onsetLatency   = 15 * time.Millisecond // capture buffering and detection delay, subtracted from timestamps
)

// Onset is the moment a note or strum started, for judging rhythm.
type Onset struct {
	Seq   uint64  `json:"seq"`
	T     float64 `json:"t"` // unix milliseconds
	Level float64 `json:"level"`
}

// onsetDetector finds the start of picked notes and strums. It looks at the energy
// above ~450 Hz, where the pick attack lives, so a new strum stands out even while the
// previous chord is still ringing.
type onsetDetector struct {
	lp   float64
	hist [onsetRefBlocks]float64
	pos  int
	cool int
}

// process looks at one block of input and reports whether a new note started in it.
func (d *onsetDetector) process(block []float64) (onset bool, level float64) {
	var sq float64
	for _, x := range block {
		d.lp += 0.058 * (x - d.lp) // one-pole low-pass; what is left over is the attack
		h := x - d.lp
		sq += h * h
	}
	r := math.Sqrt(sq / float64(len(block)))
	var ref float64
	for _, v := range d.hist {
		ref = math.Max(ref, v)
	}
	d.hist[d.pos] = r
	d.pos = (d.pos + 1) % onsetRefBlocks
	if d.cool > 0 {
		d.cool--
		return false, r
	}
	if r > 0.004 && r > 1.4*ref {
		d.cool = onsetCooldown
		return true, r
	}
	return false, r
}

// addOnset records an onset (e.mu must be held), keeping the most recent ones.
func (e *Engine) addOnset(level float64) {
	e.onsetSeq++
	e.onsets = append(e.onsets, Onset{Seq: e.onsetSeq, T: float64(time.Now().Add(-onsetLatency).UnixMilli()), Level: level})
	if len(e.onsets) > 32 {
		e.onsets = e.onsets[len(e.onsets)-32:]
	}
}
