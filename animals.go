//go:build ignore

// Parked: the animal-sounds pedal concept, to be reworked later. Not part of the build.

package main

import "math"

// Animal sounds are synthesised live with a tiny source-filter voice model: a
// sawtooth "glottal" source whose pitch glides, run through resonant formant
// filters that sweep over time, plus breath noise. Because it is live, the
// voice follows the note being played on the guitar.

type formant struct{ f, bw float64 }

// resonator is a two-pole band-pass section.
type resonator struct{ y1, y2 float64 }

func (r *resonator) process(x, f, bw float64) float64 {
	c := math.Exp(-math.Pi * bw / rate)
	b1 := 2 * c * math.Cos(2*math.Pi*f/rate)
	y := (1-c)*x + b1*r.y1 - c*c*r.y2
	r.y2, r.y1 = r.y1, y
	return y
}

// animalDef describes one sound. Functions take t in [0, 1] over dur seconds.
type animalDef struct {
	dur      float64
	ref      float64 // mean pitch of the contour in Hz: a played note of this pitch plays it unshifted
	pitch    func(t float64) float64
	env      func(t float64) float64
	formants func(t float64) []formant
	noise    float64 // breath noise mixed into the source
	growl    float64 // 0..1 amplitude roughness
	drive    float64 // output saturation, 0 = clean
	gain     float64 // set at startup so every animal peaks about the same
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// attack/release envelope: linear attack over a, linear release over r.
func env(a, r float64) func(float64) float64 {
	return func(t float64) float64 { return clamp(math.Min(t/a, (1-t)/r), 0, 1) }
}

// barks returns the local position in the nth bark window, or ok=false in the gaps.
func barks(t float64) (u float64, ok bool) {
	const length, second = 0.27, 0.38
	for _, start := range []float64{0, second} {
		if u := (t - start) / length; u >= 0 && u < 1 {
			return u, true
		}
	}
	return 0, false
}

var animalDefs = map[string]*animalDef{
	// Dog: two heavy, growly barks. Low pitch dropping fast, wide open "aw" formants,
	// a snarl of amplitude roughness, a breathy burst at the start and hard saturation.
	"dog": {
		dur: 1.0, ref: 150,
		pitch: func(t float64) float64 {
			u, _ := barks(t)
			return lerp(210, 105, math.Sqrt(u))
		},
		env: func(t float64) float64 {
			u, ok := barks(t)
			if !ok {
				return 0
			}
			return math.Min(u/0.02, 1) * math.Pow(1-u, 1.3)
		},
		formants: func(t float64) []formant {
			u, _ := barks(t)
			return []formant{{lerp(550, 750, u), 200}, {lerp(950, 1150, u), 260}, {2400, 400}}
		},
		noise: 0.7, growl: 0.55, drive: 4,
	},
	// Cat: "mee-ow": pitch up then down with vibrato, mouth closed -> wide open -> closing.
	"cat": {
		dur: 0.9, ref: 560,
		pitch: func(t float64) float64 {
			base := lerp(420, 760, math.Sin(math.Pi*math.Min(t*1.4, 1)/2))
			if t > 0.5 {
				base = lerp(760, 460, (t-0.5)/0.5)
			}
			return base * (1 + 0.015*math.Sin(2*math.Pi*6.3*t))
		},
		env: env(0.12, 0.35),
		formants: func(t float64) []formant {
			if t < 0.3 { // "mee"
				return []formant{{lerp(350, 450, t/0.3), 90}, {lerp(2300, 2100, t/0.3), 180}, {3100, 250}}
			}
			u := (t - 0.3) / 0.7 // "ow"
			return []formant{{lerp(450, 850, math.Sin(math.Pi*u)), 110}, {lerp(2100, 1300, u), 160}, {2900, 250}}
		},
		noise: 0.08,
	},
	// Cow: a long low "moooo" with a nasal "m" start, slow vibrato and a sag in pitch at the end.
	"cow": {
		dur: 1.7, ref: 115,
		pitch: func(t float64) float64 {
			base := lerp(105, 135, math.Min(t/0.4, 1))
			if t > 0.7 {
				base = lerp(135, 90, (t-0.7)/0.3)
			}
			return base * (1 + 0.02*math.Sin(2*math.Pi*9.3*t))
		},
		env: env(0.1, 0.25),
		formants: func(t float64) []formant {
			open := clamp((t-0.08)/0.2, 0, 1) // "m" -> "oo"
			return []formant{{lerp(250, 430, open), 80}, {lerp(900, 800, open), 100}, {2400, 300}}
		},
		noise: 0.05,
	},
}

// synth plays one animalDef, transposing its pitch contour to follow a target scale.
type synth struct {
	def   *animalDef
	pos   int
	phase float64
	scale float64 // smoothed pitch multiplier
	res   [3]resonator
	seed  uint32
}

func newSynth(name string, scale float64) *synth {
	return &synth{def: animalDefs[name], scale: scale, seed: 12345}
}

// next returns the next sample; scale is the pitch multiplier to glide towards.
// ok is false once the sound is finished.
func (s *synth) next(scale float64) (y float64, ok bool) {
	d := s.def
	n := int(d.dur * rate)
	if s.pos >= n {
		return 0, false
	}
	t := float64(s.pos) / float64(n)
	s.scale += 0.002 * (scale - s.scale) // ~10 ms glide so new notes don't click
	s.phase += d.pitch(t) * s.scale / rate
	s.phase -= math.Floor(s.phase)
	s.seed = s.seed*1664525 + 1013904223
	hiss := float64(int32(s.seed)) / (1 << 31)
	src := (2*s.phase - 1) + d.noise*hiss
	for i, f := range d.formants(t) {
		y += s.res[i].process(src, f.f, f.bw)
	}
	if d.growl > 0 {
		y *= 1 - d.growl*0.5*(1+math.Sin(2*math.Pi*70*float64(s.pos)/rate))
	}
	if d.drive > 0 {
		y = math.Tanh(d.drive * y)
	}
	s.pos++
	return y * d.env(t) * d.gain, true
}

func init() {
	// Level every animal: render once at its natural pitch and scale to a common peak.
	for name, d := range animalDefs {
		d.gain = 1
		s := newSynth(name, 1)
		var peak float64
		for {
			y, ok := s.next(1)
			if !ok {
				break
			}
			peak = math.Max(peak, math.Abs(y))
		}
		d.gain = 0.8 / peak
	}
}
