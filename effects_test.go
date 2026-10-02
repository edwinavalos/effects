package main

import (
	"math"
	"testing"
)

// Every pedal, switched on alone, must stay finite and at a sane level on a guitar-ish signal.
func TestPedalsStable(t *testing.T) {
	on := map[string]func(*Params){
		"tremolo": func(p *Params) { p.TremOn = true }, "chorus": func(p *Params) { p.ChorOn = true },
		"wah": func(p *Params) { p.WahOn = true }, "octave": func(p *Params) { p.OctOn = true },
		"cab": func(p *Params) { p.CabOn = true }, "comp": func(p *Params) { p.CompOn = true },
		"flanger": func(p *Params) { p.FlaOn = true }, "harmony": func(p *Params) { p.HarmOn = true },
		"ring": func(p *Params) { p.RingOn = true }, "freeze": func(p *Params) { p.FrzOn = true },
	}
	for name, enable := range on {
		p := defaultParams()
		p.GateOn, p.DriveOn, p.DelayOn, p.Volume = false, false, false, 1
		enable(&p)
		p.Order = []string{name}
		p.sanitize()
		var d dsp
		var peak float64
		for i := 0; i < rate*2; i++ {
			if i == rate/2 && name == "freeze" { // freeze after half a second of playing
				p.FrzOn = true
			} else if i < rate/2 && name == "freeze" {
				p.FrzOn = false
			}
			x := 0.3 * math.Sin(2*math.Pi*196*float64(i)/rate) * math.Exp(-float64(i%24000)/12000)
			y := d.process(x, &p)
			if math.IsNaN(y) || math.IsInf(y, 0) {
				t.Fatalf("%s: NaN/Inf at %d", name, i)
			}
			peak = math.Max(peak, math.Abs(y))
		}
		if peak < 0.01 || peak > 3 {
			t.Errorf("%s: peak %v out of range", name, peak)
		}
	}
}

func TestFixOrder(t *testing.T) {
	p := Params{Order: []string{"reverb", "bogus", "fuzz", "reverb", "tremolo"}}
	p.fixOrder()
	want := []string{"reverb", "fuzz", "tremolo"}
	if len(p.Order) != 3 || p.Order[0] != want[0] || p.Order[1] != want[1] || p.Order[2] != want[2] {
		t.Fatalf("got %v want %v", p.Order, want)
	}
	p.Order = nil
	p.fixOrder()
	if p.Order == nil || len(p.Order) != 0 {
		t.Fatalf("empty board should stay empty, got %#v", p.Order)
	}
}
