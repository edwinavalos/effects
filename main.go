// Tiny software guitar pedal with a web UI and a guitar tuner.
// guitar -> gate -> fuzz -> overdrive -> delay -> reverb -> speakers, or tuner mode (output muted).
// Audio I/O goes through parec/pacat (PipeWire/PulseAudio), so no cgo is needed.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"time"
)

const (
	rate      = 48000
	blockSize = 128 // samples per block (~2.7 ms)
	maxDelay  = 1000
)

// Params are the knobs the UI controls.
type Params struct {
	Mode     string  `json:"mode"` // "pedal" or "tuner"
	Tuning   string  `json:"tuning"`
	Volume   float64 `json:"volume"`
	RevOn    bool    `json:"revOn"`
	Room     float64 `json:"room"`
	Damp     float64 `json:"damp"`
	RevMix   float64 `json:"revMix"`
	GateOn   bool    `json:"gateOn"`
	Gate     float64 `json:"gate"`
	FuzzOn   bool    `json:"fuzzOn"`
	Fuzz     float64 `json:"fuzz"`
	FuzzTone float64 `json:"fuzzTone"`
	DriveOn  bool    `json:"driveOn"`
	Drive    float64 `json:"drive"`
	DelayOn  bool    `json:"delayOn"`
	DelayMs  float64 `json:"delayMs"`
	Feedback float64 `json:"feedback"`
	Mix      float64 `json:"mix"`
}

func defaultParams() Params {
	return Params{Mode: "pedal", Tuning: "standard", Volume: 0.5, GateOn: true, Gate: 0.01,
		Room: 0.6, Damp: 0.5, RevMix: 0.3, Fuzz: 30, FuzzTone: 0.5, DriveOn: true, Drive: 8, DelayOn: true, DelayMs: 300, Feedback: 0.35, Mix: 0.4}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func (p *Params) sanitize() {
	if p.Mode != "tuner" {
		p.Mode = "pedal"
	}
	if _, ok := tunings[p.Tuning]; !ok {
		p.Tuning = "standard"
	}
	p.Volume = clamp(p.Volume, 0, 1)
	p.Gate = clamp(p.Gate, 0, 0.2)
	p.Room = clamp(p.Room, 0, 1)
	p.Damp = clamp(p.Damp, 0, 1)
	p.RevMix = clamp(p.RevMix, 0, 1)
	p.Fuzz = clamp(p.Fuzz, 1, 100)
	p.FuzzTone = clamp(p.FuzzTone, 0, 1)
	p.Drive = clamp(p.Drive, 1, 50)
	p.DelayMs = clamp(p.DelayMs, 20, maxDelay)
	p.Feedback = clamp(p.Feedback, 0, 0.9)
	p.Mix = clamp(p.Mix, 0, 1)
}

// Engine holds the shared state between the audio loop, tuner and web UI.
type Engine struct {
	mu     sync.Mutex
	p      Params
	level  float64
	tuner  Reading
	ring   []float64 // recent raw input for the tuner
	ringAt int
}

func (e *Engine) params() Params { e.mu.Lock(); defer e.mu.Unlock(); return e.p }

// update applies fn to a copy of the params and stores the sanitized result.
func (e *Engine) update(fn func(*Params)) Params {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.p
	fn(&p)
	p.sanitize()
	e.p = p
	return p
}

// runTuner analyses the recent input ~20 times a second.
func (e *Engine) runTuner() {
	local := make([]float64, 4096)
	var smooth float64
	for range time.Tick(50 * time.Millisecond) {
		e.mu.Lock()
		n := copy(local, e.ring[e.ringAt:])
		copy(local[n:], e.ring[:e.ringAt])
		tuning := e.p.Tuning
		e.mu.Unlock()

		freq, ok := detectPitch(local)
		var r Reading
		if ok {
			// Light smoothing; reset when the pitch jumps (new note).
			if smooth > 0 && math.Abs(freq/smooth-1) < 0.03 {
				smooth = 0.7*smooth + 0.3*freq
			} else {
				smooth = freq
			}
			r = nearestString(smooth, tuning)
		} else {
			smooth = 0
		}
		e.mu.Lock()
		e.tuner = r
		e.mu.Unlock()
	}
}

// dsp holds effect state that lives only in the audio loop.
type dsp struct {
	gateEnv  float64
	fuzzLP   float64 // tone low-pass state
	dcIn     float64 // DC blocker state
	dcOut    float64
	rev      *reverb
	delayBuf [rate * maxDelay / 1000]float64
	delayAt  int
}

func (d *dsp) process(x float64, p *Params) float64 {
	if p.GateOn {
		d.gateEnv = math.Max(math.Abs(x), d.gateEnv*0.999)
		if d.gateEnv < p.Gate {
			x = 0
		}
	}
	if p.FuzzOn {
		x = d.fuzz(x, p)
	}
	if p.DriveOn {
		x = math.Tanh(x * p.Drive)
	}
	if p.DelayOn {
		n := int(p.DelayMs) * rate / 1000
		read := (d.delayAt - n + len(d.delayBuf)) % len(d.delayBuf)
		echo := d.delayBuf[read]
		d.delayBuf[d.delayAt] = x + echo*p.Feedback
		x += echo * p.Mix
	} else {
		d.delayBuf[d.delayAt] = 0
	}
	d.delayAt = (d.delayAt + 1) % len(d.delayBuf)
	if d.rev == nil {
		d.rev = newReverb()
	}
	// Keep running when off (fed silence) so the tail dies out instead of freezing.
	in := 0.0
	if p.RevOn {
		in = x
	}
	wet := d.rev.process(in, p.Room, p.Damp)
	if p.RevOn {
		x = x*(1-p.RevMix*0.5) + wet*p.RevMix
	}
	return x * p.Volume
}

// fuzz is a high-gain asymmetric clipper (positive half clips harder than the
// negative one, giving a gnarly even-harmonic buzz), followed by a DC blocker
// and a tone low-pass sweeping 800 Hz - 8 kHz.
func (d *dsp) fuzz(x float64, p *Params) float64 {
	v := x*p.Fuzz + 0.2 // bias makes the clipping asymmetric
	if v > 0 {
		v = math.Tanh(v * 3)
	} else {
		v = math.Tanh(v)
	}
	d.dcOut = v - d.dcIn + 0.995*d.dcOut
	d.dcIn = v
	fc := 800 * math.Pow(10, p.FuzzTone) // 800 Hz .. 8 kHz
	a := 1 - math.Exp(-2*math.Pi*fc/rate)
	d.fuzzLP += a * (d.dcOut - d.fuzzLP)
	return d.fuzzLP * 0.5
}

func main() {
	in := flag.String("in", "alsa_input.usb-Hercules_Rocksmith_USB_Guitar_Adapter-00.mono-fallback", "input source")
	out := flag.String("out", "", "output sink (default: system default)")
	addr := flag.String("addr", "127.0.0.1:8765", "web UI address")
	flag.Parse()

	eng := &Engine{p: defaultParams(), ring: make([]float64, 4096)}
	go eng.runTuner()
	go serve(eng, *addr)

	common := []string{"--format=s16le", fmt.Sprint("--rate=", rate), "--channels=1", "--latency-msec=10"}
	rec := exec.Command("parec", append(common, "--device="+*in)...)
	playArgs := append(common, "--client-name=goeffects")
	if *out != "" {
		playArgs = append(playArgs, "--device="+*out)
	}
	play := exec.Command("pacat", playArgs...)
	rec.Stderr, play.Stderr = os.Stderr, os.Stderr

	src, err := rec.StdoutPipe()
	check(err)
	dst, err := play.StdinPipe()
	check(err)
	check(rec.Start())
	check(play.Start())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() { <-sig; rec.Process.Kill(); play.Process.Kill(); os.Exit(0) }()

	fmt.Fprintf(os.Stderr, "open http://%s  (ctrl+c to stop; use headphones or keep volume low to avoid feedback)\n", *addr)

	var fx dsp
	raw := make([]byte, blockSize*2)
	block := make([]float64, blockSize)
	for {
		if _, err := io.ReadFull(src, raw); err != nil {
			check(err)
		}
		p := eng.params()
		var peak float64
		for i := range block {
			x := float64(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768
			block[i] = x
			peak = math.Max(peak, math.Abs(x))
			if p.Mode == "tuner" {
				x = 0 // mute output while tuning
			} else {
				x = clamp(fx.process(x, &p), -1, 1)
			}
			binary.LittleEndian.PutUint16(raw[i*2:], uint16(int16(x*32767)))
		}
		eng.mu.Lock()
		eng.level = peak
		for _, v := range block {
			eng.ring[eng.ringAt] = v
			eng.ringAt = (eng.ringAt + 1) % len(eng.ring)
		}
		eng.mu.Unlock()
		_, err := dst.Write(raw)
		check(err)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nerror:", err)
		os.Exit(1)
	}
}
