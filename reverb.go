package main

// Freeverb-style mono reverb: 8 parallel damped comb filters into 4 series
// allpass filters. Delay lengths are the classic 44.1 kHz ones, rescaled.
var (
	combLens    = [...]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	allpassLens = [...]int{556, 441, 341, 225}
)

type comb struct {
	buf   []float64
	at    int
	store float64
}

func (c *comb) process(in, feedback, damp float64) float64 {
	out := c.buf[c.at]
	c.store = out*(1-damp) + c.store*damp
	c.buf[c.at] = in + c.store*feedback
	c.at = (c.at + 1) % len(c.buf)
	return out
}

type allpass struct {
	buf []float64
	at  int
}

func (a *allpass) process(in float64) float64 {
	out := a.buf[a.at]
	a.buf[a.at] = in + out*0.5
	a.at = (a.at + 1) % len(a.buf)
	return out - in
}

type reverb struct {
	combs   [len(combLens)]comb
	allpass [len(allpassLens)]allpass
}

func newReverb() *reverb {
	r := &reverb{}
	scale := func(n int) int { return n * rate / 44100 }
	for i, n := range combLens {
		r.combs[i].buf = make([]float64, scale(n))
	}
	for i, n := range allpassLens {
		r.allpass[i].buf = make([]float64, scale(n))
	}
	return r
}

// process returns the wet signal only. room and damp are 0..1.
func (r *reverb) process(x, room, damp float64) float64 {
	feedback := 0.7 + 0.28*room
	in := x * 0.015
	var sum float64
	for i := range r.combs {
		sum += r.combs[i].process(in, feedback, damp*0.4)
	}
	for i := range r.allpass {
		sum = r.allpass[i].process(sum)
	}
	return sum * 3
}
