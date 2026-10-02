# effects

A software guitar pedal written in Go, with a web UI and a built-in tuner.
Plug a guitar into a USB audio interface, run it, and tweak the sound from your browser.

> **Work in progress.** This is a hobby project that I'm building for fun and sharing
> because it's cool. Expect rough edges, missing features and breaking changes.
> Issues and PRs are welcome!

## Features

- **Pedalboard:** 16 pedals with dials and footswitches: noise gate, fuzz, distortion, overdrive, delay, reverb, compressor, auto wah, tremolo, chorus, flanger, octave, harmony, robot (ring mod), freeze and cabinet sim. Drag pedals between the board and the tray (or use ◀ ▶) to build and order your signal chain.
- **Visualizers:** 8 scrollable visualizers (← →), full screen with F
- **Guitar tuner:** pitch detection (YIN) that snaps to the nearest string, with Standard, Drop D and Half-step-down tunings. Output is muted while tuning.
- **Scale practice:** pick a key and scale (major, minor, pentatonics, blues); it shows the notes in standard notation with guitar tab underneath, listens to what you play and advances as you hit each note. Your effects stay on. Only notes up to B4 are used, because that is where the pitch detector stops.
- **Input selector:** pick the capture device in the UI; the guitar is auto-detected and reconnects when you replug it
- **No cgo, no dependencies:** just the Go standard library

## Requirements

- Linux with PipeWire or PulseAudio (uses `parec` and `pacat`)
- Go 1.22+
- A guitar audio input. It is developed against a Rocksmith USB Guitar Adapter, but any input works.

## Run it

```sh
go build -o pedal .
./pedal
```

Then open <http://127.0.0.1:8765>.

Find your device names with `pactl list short sources` and `pactl list short sinks`:

```sh
./pedal -in <source-name> -out <sink-name> -addr 127.0.0.1:8765
```

By default the guitar interface is auto-detected (or pick a device in the UI). Use headphones, or keep the volume low, to avoid feedback.

## How it works

Audio is piped through `parec` and `pacat` (10 ms buffers) and processed in 128-sample blocks.
Expect roughly 20-40 ms of round-trip latency. Getting this lower (JACK/PipeWire directly) is on the wish list.

| File | What it does |
| --- | --- |
| `main.go` | Audio loop, parameters, gate / fuzz / distortion / overdrive / delay |
| `effects.go` | Tremolo, chorus, wah, octave, cabinet, compressor, flanger, harmony, ring mod, freeze |
| `spectrum.go`, `audio.go` | Visualizer spectrum, capture device detection |
| `reverb.go` | Freeverb-style reverb |
| `tuner.go` | YIN pitch detection and guitar tunings |
| `server.go`, `index.html` | Web server and UI (including the scale-practice notation and tab, which run in the browser) |

## Ideas / TODO

- Lower latency audio backend
- More effects (phaser, reverse)
- Presets
- Better tuner smoothing

## License

[MIT](LICENSE)
