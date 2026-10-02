package main

import (
	"math"
	"time"
)

const (
	nBands    = 32
	specSize  = 1024
	specLoHz  = 80.0
	specHiHz  = 8000.0
	specFloor = -60.0 // dB shown as an empty bar
)

// runSpectrum turns the recent output into nBands log-spaced bar heights (0..1)
// about 30 times a second, for the visualizer.
func (e *Engine) runSpectrum() {
	local := make([]float64, len(e.outRing))
	win := make([]float64, specSize)
	for i := range win {
		win[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/(specSize-1))
	}
	coef := make([]float64, nBands)
	for b := range coef {
		f := specLoHz * math.Pow(specHiHz/specLoHz, (float64(b)+0.5)/nBands)
		coef[b] = 2 * math.Cos(2*math.Pi*f/rate)
	}
	bars := make([]float64, nBands)
	for range time.Tick(33 * time.Millisecond) {
		e.mu.Lock()
		n := copy(local, e.outRing[e.outAt:])
		copy(local[n:], e.outRing[:e.outAt])
		e.mu.Unlock()
		recent := local[len(local)-specSize:]

		for b := range bars {
			// Goertzel filter: the energy at one frequency per bar.
			var s1, s2 float64
			for i, x := range recent {
				s0 := x*win[i] + coef[b]*s1 - s2
				s2, s1 = s1, s0
			}
			power := s1*s1 + s2*s2 - coef[b]*s1*s2
			amp := 4 * math.Sqrt(math.Max(power, 0)) / specSize // ~ sine amplitude
			db := 20*math.Log10(amp+1e-9) + 6*float64(b)/nBands // slight treble tilt
			bars[b] = clamp((db-specFloor)/-specFloor, 0, 1)
		}
		e.mu.Lock()
		e.spectrum = append(e.spectrum[:0], bars...)
		e.mu.Unlock()
	}
}
