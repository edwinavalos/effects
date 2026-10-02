// Tiny software guitar pedal with a web UI and a guitar tuner.
// guitar -> gate -> fuzz -> distortion -> overdrive -> delay -> reverb -> speakers, or tuner mode (output muted).
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
	"slices"
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
	Mode      string  `json:"mode"` // "pedal" or "tuner"
	Tuning    string  `json:"tuning"`
	Volume    float64 `json:"volume"`
	RevOn     bool    `json:"revOn"`
	Room      float64 `json:"room"`
	Damp      float64 `json:"damp"`
	RevMix    float64 `json:"revMix"`
	GateOn    bool    `json:"gateOn"`
	Gate      float64 `json:"gate"`
	FuzzOn    bool    `json:"fuzzOn"`
	Fuzz      float64 `json:"fuzz"`
	FuzzTone  float64 `json:"fuzzTone"`
	DistOn    bool    `json:"distOn"`
	Dist      float64 `json:"dist"`
	DistTone  float64 `json:"distTone"`
	DistLevel float64 `json:"distLevel"`
	DriveOn   bool    `json:"driveOn"`
	Drive     float64 `json:"drive"`
	DelayOn   bool    `json:"delayOn"`
	DelayMs   float64 `json:"delayMs"`
	Feedback  float64 `json:"feedback"`
	Mix       float64 `json:"mix"`

	FxParams

	Order []string `json:"order"` // pedals on the board, first to last; the rest are in the tray
}

// effectIDs are all known pedals. Only those in Params.Order are in the signal chain.
var effectIDs = []string{"gate", "fuzz", "dist", "drive", "delay", "reverb",
	"tremolo", "chorus", "wah", "octave", "cab", "comp", "flanger", "harmony", "ring", "freeze"}

var defaultOrder = []string{"gate", "fuzz", "dist", "drive", "delay", "reverb"}

func defaultParams() Params {
	return Params{Mode: "pedal", Tuning: "standard", Volume: 0.5, GateOn: true, Gate: 0.01,
		Room: 0.6, Damp: 0.5, RevMix: 0.3, Fuzz: 30, FuzzTone: 0.5, Dist: 20, DistTone: 0.5, DistLevel: 0.5, DriveOn: true, Drive: 8, DelayOn: true, DelayMs: 300, Feedback: 0.35, Mix: 0.4,
		FxParams: defaultFx(), Order: slices.Clone(defaultOrder)}
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
	p.Dist = clamp(p.Dist, 1, 100)
	p.DistTone = clamp(p.DistTone, 0, 1)
	p.DistLevel = clamp(p.DistLevel, 0, 1)
	p.Drive = clamp(p.Drive, 1, 50)
	p.DelayMs = clamp(p.DelayMs, 20, maxDelay)
	p.Feedback = clamp(p.Feedback, 0, 0.9)
	p.Mix = clamp(p.Mix, 0, 1)
	p.FxParams.sanitize()
	p.fixOrder()
}

// fixOrder keeps only known, non-repeated pedal names. It always builds a new
// slice, so copies of Params held by the audio loop are never modified.
func (p *Params) fixOrder() {
	out := make([]string, 0, len(effectIDs))
	for _, name := range p.Order {
		if slices.Contains(effectIDs, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	p.Order = out
}

// Engine holds the shared state between the audio loop, tuner and web UI.
type Engine struct {
	mu     sync.Mutex
	p      Params
	level  float64
	tuner  Reading
	ring   []float64 // recent raw input for the tuner
	ringAt int

	chordRing []float64 // longer history of raw input for chord recognition
	chordAt   int
	chord     ChordReading

	onsets   []Onset // recent note/strum starts, for rhythm practice
	onsetSeq uint64

	outRing  []float64 // recent output for the visualizer
	outAt    int
	spectrum []float64

	want      string        // source chosen in the UI ("" = auto-detect the guitar)
	active    string        // source currently being captured, "" if disconnected
	switchSrc chan struct{} // poked when the UI picks another source
	saveCh    chan struct{} // poked when params change, if autosave is on
}

// setSource selects the capture device ("" = auto) and wakes the audio loop.
func (e *Engine) setSource(name string) {
	e.mu.Lock()
	e.want = name
	e.mu.Unlock()
	select {
	case e.switchSrc <- struct{}{}:
	default:
	}
}

func (e *Engine) setActive(name string) { e.mu.Lock(); e.active = name; e.mu.Unlock() }

func (e *Engine) params() Params { e.mu.Lock(); defer e.mu.Unlock(); return e.p }

// update applies fn to a copy of the params and stores the sanitized result.
func (e *Engine) update(fn func(*Params)) Params {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.p
	p.Order = slices.Clone(p.Order) // the JSON decoder would reuse the shared backing array
	fn(&p)
	p.sanitize()
	e.p = p
	select {
	case e.saveCh <- struct{}{}:
	default:
	}
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
	gateEnv float64
	fuzzLP  float64 // tone low-pass state
	dcIn    float64 // DC blocker state
	dcOut   float64
	distHP  float64 // distortion pre-emphasis high-pass state
	distIn  float64
	distLP  float64 // distortion tone low-pass state
	rev     *reverb
	fx
	delayBuf [rate * maxDelay / 1000]float64
	delayAt  int
}

func (d *dsp) process(x float64, p *Params) float64 {
	for _, name := range p.Order {
		switch name {
		case "gate":
			if p.GateOn {
				d.gateEnv = math.Max(math.Abs(x), d.gateEnv*0.999)
				if d.gateEnv < p.Gate {
					x = 0
				}
			}
		case "fuzz":
			if p.FuzzOn {
				x = d.fuzz(x, p)
			}
		case "dist":
			if p.DistOn {
				x = d.distortion(x, p)
			}
		case "drive":
			if p.DriveOn {
				x = math.Tanh(x * p.Drive)
			}
		case "delay":
			x = d.delay(x, p)
		case "reverb":
			x = d.reverb(x, p)
		case "tremolo":
			if p.TremOn {
				x = d.trem.process(x, p)
			}
		case "chorus":
			if p.ChorOn {
				x = d.chor.process(x, p)
			}
		case "wah":
			if p.WahOn {
				x = d.wah.process(x, p)
			}
		case "octave":
			if p.OctOn {
				x = d.oct.process(x, p)
			}
		case "cab":
			if p.CabOn {
				x = d.cab.process(x, p)
			}
		case "comp":
			if p.CompOn {
				x = d.comp.process(x, p)
			}
		case "flanger":
			if p.FlaOn {
				x = d.flan.process(x, p)
			}
		case "harmony":
			if p.HarmOn {
				x = d.harm.process(x, p)
			}
		case "ring":
			if p.RingOn {
				x = d.ring.process(x, p)
			}
		case "freeze":
			x = d.frz.process(x, p) // keeps recording while off
		}
	}
	return x * p.Volume
}

func (d *dsp) delay(x float64, p *Params) float64 {
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
	return x
}

func (d *dsp) reverb(x float64, p *Params) float64 {
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
	return x
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

// distortion is a tighter, harder-edged sound than fuzz/overdrive: a high-pass
// before the gain stage keeps the low end from turning to mud, a cubic
// clipper with a flat top gives a crunchy hard-clip edge, then a tone low-pass
// sweeps 1 kHz - 6 kHz.
func (d *dsp) distortion(x float64, p *Params) float64 {
	const hpA = 0.985 // ~110 Hz one-pole high-pass
	d.distHP = hpA * (d.distHP + x - d.distIn)
	d.distIn = x
	v := clamp(d.distHP*p.Dist, -1, 1)
	v = 1.5 * (v - v*v*v/3) // in [-1, 1], flat at the rails
	fc := 1000 * math.Pow(6, p.DistTone)
	a := 1 - math.Exp(-2*math.Pi*fc/rate)
	d.distLP += a * (v - d.distLP)
	return d.distLP * p.DistLevel
}

func main() {
	in := flag.String("in", "", "input source (default: auto-detect the guitar interface)")
	out := flag.String("out", "", "output sink (default: system default)")
	addr := flag.String("addr", "127.0.0.1:8765", "web UI address")
	statePath := flag.String("state", defaultStatePath(), "file that remembers the pedalboard between runs (\"\" = don't save)")
	flag.Parse()

	startParams := defaultParams()
	if *statePath != "" {
		startParams = loadParams(*statePath)
	}
	eng := &Engine{p: startParams, ring: make([]float64, 4096), chordRing: make([]float64, chordWindow), outRing: make([]float64, 2048), want: *in, switchSrc: make(chan struct{}, 1)}
	if *statePath != "" {
		eng.autosave(*statePath)
	}
	go eng.runTuner()
	go eng.runChords()
	go eng.runSpectrum()
	go serve(eng, *addr)

	common := []string{"--format=s16le", fmt.Sprint("--rate=", rate), "--channels=1", "--latency-msec=10"}
	playArgs := append(common, "--client-name=goeffects")
	if *out != "" {
		playArgs = append(playArgs, "--device="+*out)
	}
	play := exec.Command("pacat", playArgs...)
	play.Stderr = os.Stderr
	dst, err := play.StdinPipe()
	check(err)
	check(play.Start())

	var recMu sync.Mutex
	var rec *exec.Cmd
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		recMu.Lock()
		if rec != nil {
			rec.Process.Kill()
		}
		recMu.Unlock()
		play.Process.Kill()
		os.Exit(0)
	}()

	fmt.Fprintf(os.Stderr, "open http://%s  (ctrl+c to stop; use headphones or keep volume low to avoid feedback)\n", *addr)

	var fx dsp
	var od onsetDetector
	raw := make([]byte, blockSize*2)
	block := make([]float64, blockSize)
	outBlock := make([]float64, blockSize)
	silence := make([]byte, blockSize*2)
	for {
		// (Re)connect to the guitar. Keeps retrying so a replugged cable just works.
		eng.mu.Lock()
		want := eng.want
		eng.mu.Unlock()
		name := pickSource(want)
		if name == "" {
			eng.setActive("")
			select {
			case <-eng.switchSrc:
			case <-time.After(time.Second):
			}
			continue
		}
		cmd := exec.Command("parec", append(common, "--device="+name)...)
		src, err := cmd.StdoutPipe()
		check(err)
		if cmd.Start() != nil {
			time.Sleep(time.Second)
			continue
		}
		recMu.Lock()
		rec = cmd
		recMu.Unlock()
		eng.setActive(name)
		fmt.Fprintln(os.Stderr, "capturing from", name)
		done := make(chan struct{})
		go func() { // the UI picked another device: drop this capture
			select {
			case <-eng.switchSrc:
				cmd.Process.Kill()
			case <-done:
			}
		}()

		for {
			if _, err := io.ReadFull(src, raw); err != nil {
				break
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
				outBlock[i] = x
			}
			onset, onsetLevel := od.process(block)
			eng.mu.Lock()
			if onset {
				eng.addOnset(onsetLevel)
			}
			for _, v := range outBlock {
				eng.outRing[eng.outAt] = v
				eng.outAt = (eng.outAt + 1) % len(eng.outRing)
			}
			eng.level = peak
			for _, v := range block {
				eng.ring[eng.ringAt] = v
				eng.ringAt = (eng.ringAt + 1) % len(eng.ring)
				eng.chordRing[eng.chordAt] = v
				eng.chordAt = (eng.chordAt + 1) % len(eng.chordRing)
			}
			eng.mu.Unlock()
			_, err := dst.Write(raw)
			check(err)
		}
		close(done)
		cmd.Process.Kill()
		cmd.Wait()
		eng.setActive("")
		eng.mu.Lock()
		eng.level = 0
		clear(eng.outRing)
		eng.mu.Unlock()
		dst.Write(silence)
		fmt.Fprintln(os.Stderr, "guitar disconnected, waiting...")
		time.Sleep(500 * time.Millisecond)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nerror:", err)
		os.Exit(1)
	}
}
