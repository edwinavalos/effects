package main

import "math"

// FxParams are the knobs of the extra pedals (embedded in Params, so the JSON is flat).
type FxParams struct {
	TremOn    bool    `json:"tremOn"`
	TremRate  float64 `json:"tremRate"` // Hz
	TremDepth float64 `json:"tremDepth"`

	ChorOn    bool    `json:"chorOn"`
	ChorRate  float64 `json:"chorRate"` // Hz
	ChorDepth float64 `json:"chorDepth"`
	ChorMix   float64 `json:"chorMix"`

	WahOn   bool    `json:"wahOn"`
	WahSens float64 `json:"wahSens"`
	WahFreq float64 `json:"wahFreq"`
	WahQ    float64 `json:"wahQ"`

	OctOn   bool    `json:"octOn"`
	OctDown float64 `json:"octDown"`
	OctUp   float64 `json:"octUp"`
	OctDry  float64 `json:"octDry"`

	CabOn   bool    `json:"cabOn"`
	CabTone float64 `json:"cabTone"`
	CabBody float64 `json:"cabBody"`

	CompOn     bool    `json:"compOn"`
	CompThresh float64 `json:"compThresh"` // dB
	CompRatio  float64 `json:"compRatio"`
	CompMakeup float64 `json:"compMakeup"` // dB

	FlaOn       bool    `json:"flaOn"`
	FlaRate     float64 `json:"flaRate"` // Hz
	FlaDepth    float64 `json:"flaDepth"`
	FlaFeedback float64 `json:"flaFeedback"`
	FlaMix      float64 `json:"flaMix"`

	HarmOn   bool    `json:"harmOn"`
	HarmSemi float64 `json:"harmSemi"` // semitones
	HarmMix  float64 `json:"harmMix"`

	RingOn   bool    `json:"ringOn"`
	RingFreq float64 `json:"ringFreq"` // Hz
	RingMix  float64 `json:"ringMix"`

	FrzOn   bool    `json:"frzOn"`
	FrzSize float64 `json:"frzSize"` // ms of sound that gets looped
	FrzDry  float64 `json:"frzDry"`
}

func defaultFx() FxParams {
	return FxParams{
		TremRate: 5, TremDepth: 0.6,
		ChorRate: 0.8, ChorDepth: 0.5, ChorMix: 0.5,
		WahSens: 0.7, WahFreq: 0.4, WahQ: 0.5,
		OctDown: 0.7, OctUp: 0.3, OctDry: 1,
		CabTone: 0.5, CabBody: 0.4,
		CompThresh: -20, CompRatio: 4, CompMakeup: 6,
		FlaRate: 0.3, FlaDepth: 0.7, FlaFeedback: 0.5, FlaMix: 0.5,
		HarmSemi: 7, HarmMix: 0.5,
		RingFreq: 300, RingMix: 0.6,
		FrzSize: 150, FrzDry: 0.5,
	}
}

func (f *FxParams) sanitize() {
	f.TremRate = clamp(f.TremRate, 0.1, 15)
	f.TremDepth = clamp(f.TremDepth, 0, 1)
	f.ChorRate = clamp(f.ChorRate, 0.05, 6)
	f.ChorDepth = clamp(f.ChorDepth, 0, 1)
	f.ChorMix = clamp(f.ChorMix, 0, 1)
	f.WahSens = clamp(f.WahSens, 0, 1)
	f.WahFreq = clamp(f.WahFreq, 0, 1)
	f.WahQ = clamp(f.WahQ, 0, 1)
	f.OctDown = clamp(f.OctDown, 0, 1)
	f.OctUp = clamp(f.OctUp, 0, 1)
	f.OctDry = clamp(f.OctDry, 0, 1)
	f.CabTone = clamp(f.CabTone, 0, 1)
	f.CabBody = clamp(f.CabBody, 0, 1)
	f.CompThresh = clamp(f.CompThresh, -50, 0)
	f.CompRatio = clamp(f.CompRatio, 1, 20)
	f.CompMakeup = clamp(f.CompMakeup, 0, 24)
	f.FlaRate = clamp(f.FlaRate, 0.05, 5)
	f.FlaDepth = clamp(f.FlaDepth, 0, 1)
	f.FlaFeedback = clamp(f.FlaFeedback, 0, 0.9)
	f.FlaMix = clamp(f.FlaMix, 0, 1)
	f.HarmSemi = clamp(math.Round(f.HarmSemi), -12, 12)
	f.HarmMix = clamp(f.HarmMix, 0, 1)
	f.RingFreq = clamp(f.RingFreq, 20, 2000)
	f.RingMix = clamp(f.RingMix, 0, 1)
	f.FrzSize = clamp(f.FrzSize, 50, 500)
	f.FrzDry = clamp(f.FrzDry, 0, 1)
}

// fx holds the audio-loop state of the extra pedals.
type fx struct {
	trem tremolo
	chor chorus
	wah  wah
	oct  octave
	cab  cab
	comp comp
	flan flanger
	harm harmony
	ring ringMod
	frz  freeze
}

// modDelay is a delay line with fractional (linearly interpolated) reads.
type modDelay struct {
	buf []float64
	at  int
}

func (m *modDelay) push(x float64) {
	if m.buf == nil {
		m.buf = make([]float64, 8192)
	}
	m.buf[m.at] = x
	m.at = (m.at + 1) % len(m.buf)
}

// read returns the sample d samples ago (d >= 1 is the latest pushed sample).
func (m *modDelay) read(d float64) float64 {
	if m.buf == nil {
		return 0
	}
	n := len(m.buf)
	pos := float64(m.at) - d
	for pos < 0 {
		pos += float64(n)
	}
	i := int(pos)
	f := pos - float64(i)
	return m.buf[i%n]*(1-f) + m.buf[(i+1)%n]*f
}

func advance(ph *float64, hz float64) float64 {
	*ph += hz / rate
	*ph -= math.Floor(*ph)
	return *ph
}

// Tremolo: an LFO wobbles the volume.
type tremolo struct{ ph float64 }

func (t *tremolo) process(x float64, p *Params) float64 {
	ph := advance(&t.ph, p.TremRate)
	return x * (1 - p.TremDepth*0.5*(1+math.Sin(2*math.Pi*ph)))
}

// Chorus: a short delay swept by an LFO, mixed with the dry signal.
type chorus struct {
	d  modDelay
	ph float64
}

func (c *chorus) process(x float64, p *Params) float64 {
	c.d.push(x)
	lfo := math.Sin(2 * math.Pi * advance(&c.ph, p.ChorRate))
	wet := c.d.read((0.015 + 0.007*p.ChorDepth*lfo) * rate)
	return (x + p.ChorMix*wet) / (1 + 0.5*p.ChorMix)
}

// Auto-wah: a resonant band-pass (state variable filter) whose centre frequency
// follows how hard you pick.
type wah struct{ env, low, band float64 }

func (w *wah) process(x float64, p *Params) float64 {
	a := math.Abs(x)
	if a > w.env {
		w.env += 0.004 * (a - w.env)
	} else {
		w.env += 0.0002 * (a - w.env)
	}
	open := clamp(w.env*p.WahSens*8, 0, 1)
	fc := (300 + 500*p.WahFreq) * math.Pow(6, open)
	q := 1 / (2 + 8*p.WahQ)
	f := 2 * math.Sin(math.Pi*math.Min(fc, 7000)/rate)
	w.low += f * w.band
	high := x - w.low - q*w.band
	w.band += f * high
	return w.band*q*2.5 + 0.25*x
}

// Octave: a flip-flop divider gives a square one octave down (scaled by the
// input envelope), full-wave rectification gives the octave up.
type octave struct {
	lp, env, dn float64
	flip        float64
	armed       bool
	hpIn, hpOut float64
}

func (o *octave) process(x float64, p *Params) float64 {
	if o.flip == 0 {
		o.flip = 1
	}
	o.env += 0.01 * (math.Abs(x) - o.env)
	o.lp += 0.1 * (x - o.lp)
	h := 0.15*o.env + 1e-4
	if !o.armed && o.lp > h {
		o.armed = true
		o.flip = -o.flip
	} else if o.armed && o.lp < -h {
		o.armed = false
	}
	o.dn += 0.15 * (o.flip*o.env*1.5 - o.dn)
	r := math.Abs(x)
	o.hpOut = r - o.hpIn + 0.995*o.hpOut
	o.hpIn = r
	return p.OctDry*x + p.OctDown*o.dn + p.OctUp*o.hpOut*2
}

// Cabinet: rolls off the lows and the harsh highs like a guitar speaker.
type cab struct{ hpIn, hpOut, lpA, lpB, low float64 }

func (c *cab) process(x float64, p *Params) float64 {
	c.hpOut = 0.99 * (c.hpOut + x - c.hpIn)
	c.hpIn = x
	a := 1 - math.Exp(-2*math.Pi*(2200+4500*p.CabTone)/rate)
	c.lpA += a * (c.hpOut - c.lpA)
	c.lpB += a * (c.lpA - c.lpB)
	c.low += 0.0194 * (c.hpOut - c.low) // ~150 Hz body
	return c.lpB + p.CabBody*1.2*c.low
}

// Compressor: feed-forward peak compressor.
type comp struct{ env float64 }

func (c *comp) process(x float64, p *Params) float64 {
	a := math.Abs(x)
	if a > c.env {
		c.env += 0.004 * (a - c.env)
	} else {
		c.env += 0.00014 * (a - c.env)
	}
	over := 20*math.Log10(c.env+1e-9) - p.CompThresh
	gain := p.CompMakeup
	if over > 0 {
		gain -= over * (1 - 1/p.CompRatio)
	}
	return x * math.Pow(10, gain/20)
}

// Flanger: a very short swept delay with feedback.
type flanger struct {
	d  modDelay
	ph float64
}

func (f *flanger) process(x float64, p *Params) float64 {
	lfo := math.Sin(2 * math.Pi * advance(&f.ph, p.FlaRate))
	wet := f.d.read((0.0035 + 0.003*p.FlaDepth*lfo) * rate)
	f.d.push(x + wet*p.FlaFeedback)
	return (x + p.FlaMix*wet) / (1 + 0.5*p.FlaMix)
}

// Harmony: a delay-line pitch shifter (two crossfaded taps whose delay ramps),
// mixed with the dry note for an instant harmony.
type harmony struct {
	d  modDelay
	ph float64
}

func (h *harmony) process(x float64, p *Params) float64 {
	const win = 2400.0 // samples; longer = smoother but more echo
	h.d.push(x)
	ratio := math.Pow(2, p.HarmSemi/12)
	h.ph += (1 - ratio) / win
	h.ph -= math.Floor(h.ph)
	var wet float64
	for _, off := range []float64{0, 0.5} {
		ph := h.ph + off
		ph -= math.Floor(ph)
		s := math.Sin(math.Pi * ph)
		wet += s * s * h.d.read(ph*win+1)
	}
	return (x + p.HarmMix*wet) / (1 + 0.5*p.HarmMix)
}

// Ring modulator: multiplies the guitar by a sine wave for a metallic, robotic sound.
type ringMod struct{ ph float64 }

func (r *ringMod) process(x float64, p *Params) float64 {
	return x * (1 - p.RingMix + p.RingMix*math.Sin(2*math.Pi*advance(&r.ph, p.RingFreq)))
}

// Freeze: while on, loops the last few ms of sound as a drone (two overlapping
// crossfaded read heads) and lets you play over it.
type freeze struct {
	rec  [rate / 2]float64
	at   int
	snap []float64
	t    int
	was  bool
}

func (f *freeze) process(x float64, p *Params) float64 {
	if !p.FrzOn {
		f.rec[f.at] = x
		f.at = (f.at + 1) % len(f.rec)
		f.was = false
		return x
	}
	if !f.was {
		n := int(p.FrzSize * rate / 1000)
		f.snap = make([]float64, n)
		for i := range f.snap {
			f.snap[i] = f.rec[(f.at-n+i+len(f.rec))%len(f.rec)]
		}
		f.t, f.was = 0, true
	}
	n := len(f.snap)
	var y float64
	for _, off := range []int{0, n / 2} {
		pos := (f.t + off) % n
		s := math.Sin(math.Pi * float64(pos) / float64(n))
		y += s * s * f.snap[pos]
	}
	f.t++
	return y + x*p.FrzDry
}
