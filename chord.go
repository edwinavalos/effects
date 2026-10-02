package main

import (
	"math"
	"math/cmplx"
	"time"
)

// Chord recognition: a spectrum of the last ~340 ms of the raw guitar signal is
// folded into a 12-note "chromagram", which is matched against chord templates.
const (
	chordWindow = 16384 // samples analysed (~0.34 s, 2.9 Hz per bin)
	chordLoHz   = 70.0
	chordHiHz   = 1100.0
)

// chordQualities are the chord types we can recognise, as semitone offsets from the
// root with a weight each (the fifth is often left out or weak, so it counts less).
// The keys match the quality suffixes the UI uses.
var chordQualities = []struct {
	Name  string
	Notes map[int]float64
}{
	{"", map[int]float64{0: 1, 4: 0.9, 7: 0.7}},
	{"m", map[int]float64{0: 1, 3: 0.9, 7: 0.7}},
	{"7", map[int]float64{0: 1, 4: 0.9, 7: 0.6, 10: 0.8}},
	{"maj7", map[int]float64{0: 1, 4: 0.9, 7: 0.6, 11: 0.8}},
	{"m7", map[int]float64{0: 1, 3: 0.9, 7: 0.6, 10: 0.8}},
	{"sus2", map[int]float64{0: 1, 2: 0.9, 7: 0.7}},
	{"sus4", map[int]float64{0: 1, 5: 0.9, 7: 0.7}},
}

var noteNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// ChordReading is what the UI shows for chord recognition.
type ChordReading struct {
	Active  bool      `json:"active"`
	Root    int       `json:"root"`    // pitch class, 0 = C
	Quality string    `json:"quality"` // "", "m", "7", "maj7", "m7", "sus2", "sus4"
	Name    string    `json:"name"`
	Score   float64   `json:"score"` // 0..1, how well the notes fit the chord
	Chroma  []float64 `json:"chroma"`
}

// fft is an in-place radix-2 FFT; len(x) must be a power of two.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			f := complex(1, 0)
			for k := 0; k < size/2; k++ {
				a, b := x[start+k], x[start+k+size/2]*f
				x[start+k], x[start+k+size/2] = a+b, a-b
				f *= w
			}
		}
	}
}

var chordWin = func() []float64 {
	w := make([]float64, chordWindow)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/(chordWindow-1))
	}
	return w
}()

// chromagram folds the spectral peaks of buf into 12 pitch classes (0 = C), and also
// returns the pitch class of the lowest strong peak, which is usually the chord's bass note.
func chromagram(buf []float64) (chroma [12]float64, bass int) {
	x := make([]complex128, chordWindow)
	for i, v := range buf[len(buf)-chordWindow:] {
		x[i] = complex(v*chordWin[i], 0)
	}
	fft(x)
	binHz := float64(rate) / chordWindow
	lo, hi := int(chordLoHz/binHz), int(chordHiHz/binHz)
	mag := make([]float64, hi+2)
	var maxMag float64
	for k := lo - 1; k <= hi+1; k++ {
		mag[k] = cmplx.Abs(x[k])
		if k >= lo && k <= hi {
			maxMag = math.Max(maxMag, mag[k])
		}
	}
	type peak struct{ freq, mag float64 }
	var peaks []peak
	for k := lo; k <= hi; k++ {
		if mag[k] < 0.04*maxMag || mag[k] <= mag[k-1] || mag[k] < mag[k+1] {
			continue
		}
		// Parabolic interpolation on the log magnitude for the true frequency.
		a, b, c := math.Log(mag[k-1]+1e-12), math.Log(mag[k]), math.Log(mag[k+1]+1e-12)
		off := 0.0
		if den := a - 2*b + c; den != 0 {
			off = clamp(0.5*(a-c)/den, -0.5, 0.5)
		}
		peaks = append(peaks, peak{(float64(k) + off) * binHz, mag[k]})
	}

	bassMag := 0.0
	for i, p := range peaks { // ascending frequency
		w := math.Sqrt(p.mag / maxMag)
		// A string's 3rd, 5th and 7th harmonics land on the fifth, third and flat
		// seventh of its note; discount peaks that are likely harmonics of a lower, stronger peak.
		for _, q := range peaks[:i] {
			if q.mag < p.mag*0.5 {
				continue
			}
			for _, h := range []float64{3, 5, 7} {
				if math.Abs(p.freq/(q.freq*h)-1) < 0.015 {
					w *= 0.35
				}
			}
		}
		midi := 69 + 12*math.Log2(p.freq/440)
		pc := ((int(math.Round(midi)) % 12) + 12) % 12
		chroma[pc] += w
		if p.freq < 220 && bassMag == 0 && w > 0.3 { // peaks are in ascending order: the first strong one is the lowest
			bass, bassMag = pc, w
		}
	}
	if bassMag == 0 {
		bass = -1
	}
	return
}

// detectChord recognises the chord in the tail of buf. Active is false when the
// signal is quiet or the notes don't clearly form one of the known chords.
func detectChord(buf []float64) ChordReading {
	if len(buf) < chordWindow {
		return ChordReading{}
	}
	var sq float64
	for _, v := range buf[len(buf)-chordWindow:] {
		sq += v * v
	}
	if math.Sqrt(sq/chordWindow) < 0.003 {
		return ChordReading{}
	}
	chroma, bass := chromagram(buf)
	var peak, norm float64
	for _, v := range chroma {
		peak = math.Max(peak, v)
	}
	if peak == 0 {
		return ChordReading{}
	}
	notes := 0
	for i, v := range chroma {
		chroma[i] = v / peak
		norm += chroma[i] * chroma[i]
		if chroma[i] > 0.2 {
			notes++
		}
	}
	norm = math.Sqrt(norm)

	best := ChordReading{Chroma: chroma[:]}
	for root := 0; root < 12; root++ {
		for _, q := range chordQualities {
			var dot, tn float64
			for off, w := range q.Notes {
				dot += w * chroma[(root+off)%12]
				tn += w * w
			}
			score := dot / (norm * math.Sqrt(tn))
			if root == bass {
				score *= 1.15 // the lowest note is usually the root
			}
			if score > best.Score {
				best.Root, best.Quality, best.Score = root, q.Name, score
			}
		}
	}
	best.Score = math.Min(best.Score, 1)
	best.Name = noteNames[best.Root] + best.Quality
	best.Active = notes >= 3 && best.Score >= 0.8
	if !best.Active {
		return ChordReading{Chroma: chroma[:]}
	}
	return best
}

// runChords analyses the recent raw input ~10 times a second.
func (e *Engine) runChords() {
	local := make([]float64, chordWindow)
	for range time.Tick(100 * time.Millisecond) {
		e.mu.Lock()
		n := copy(local, e.chordRing[e.chordAt:])
		copy(local[n:], e.chordRing[:e.chordAt])
		e.mu.Unlock()
		r := detectChord(local)
		e.mu.Lock()
		e.chord = r
		e.mu.Unlock()
	}
}
