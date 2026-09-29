package main

import (
	"math"
	"testing"
)

func TestDetectPitch(t *testing.T) {
	for _, f := range []float64{82.41, 110, 196, 329.63} {
		buf := make([]float64, 4096)
		for i := range buf {
			// fundamental plus harmonics, like a plucked string
			ph := 2 * math.Pi * f * float64(i) / rate
			buf[i] = 0.3*math.Sin(ph) + 0.2*math.Sin(2*ph) + 0.1*math.Sin(3*ph)
		}
		got, ok := detectPitch(buf)
		if !ok || math.Abs(1200*math.Log2(got/f)) > 2 {
			t.Errorf("f=%v got=%v ok=%v", f, got, ok)
		}
	}
	if _, ok := detectPitch(make([]float64, 4096)); ok {
		t.Error("silence detected as pitch")
	}
	if r := nearestString(112, "standard"); r.Note != "A2" || r.Cents < 0 {
		t.Errorf("nearestString: %+v", r)
	}
}

func TestFuzz(t *testing.T) {
	var d dsp
	p := defaultParams()
	p.FuzzOn, p.DriveOn, p.DelayOn, p.GateOn, p.Volume = true, false, false, false, 1
	var peak float64
	for i := 0; i < 48000; i++ {
		y := d.process(0.1*math.Sin(2*math.Pi*110*float64(i)/rate), &p)
		if math.IsNaN(y) || math.Abs(y) > 1 {
			t.Fatalf("bad sample %v", y)
		}
		peak = math.Max(peak, math.Abs(y))
	}
	if peak < 0.05 {
		t.Errorf("fuzz output too quiet: %v", peak)
	}
}

func TestReverb(t *testing.T) {
	var d dsp
	p := defaultParams()
	p.RevOn, p.DriveOn, p.DelayOn, p.GateOn, p.Volume, p.RevMix = true, false, false, false, 1, 1
	var tail float64
	for i := 0; i < rate*2; i++ {
		x := 0.0
		if i == 0 {
			x = 0.5 // impulse
		}
		y := d.process(x, &p)
		if math.IsNaN(y) || math.Abs(y) > 1 {
			t.Fatalf("bad sample %v", y)
		}
		if i > rate/2 {
			tail = math.Max(tail, math.Abs(y))
		}
	}
	if tail == 0 {
		t.Error("no reverb tail after 0.5s")
	}
}
