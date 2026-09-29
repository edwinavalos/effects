# effects

A software guitar pedal written in Go, with a web UI and a built-in tuner.
Plug a guitar into a USB audio interface, run it, and tweak the sound from your browser.

> **Work in progress.** This is a hobby project that I'm building for fun and sharing
> because it's cool. Expect rough edges, missing features and breaking changes.
> Issues and PRs are welcome!

## Features

- **Effect chain:** noise gate → fuzz → overdrive → delay → reverb
- **Guitar tuner:** pitch detection (YIN) that snaps to the nearest string, with Standard, Drop D and Half-step-down tunings. Output is muted while tuning.
- **Web UI:** knobs and switches for everything, with a live input level meter
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

The default input is the Rocksmith adapter. Use headphones, or keep the volume low, to avoid feedback.

## How it works

Audio is piped through `parec` and `pacat` (10 ms buffers) and processed in 128-sample blocks.
Expect roughly 20-40 ms of round-trip latency. Getting this lower (JACK/PipeWire directly) is on the wish list.

| File | What it does |
| --- | --- |
| `main.go` | Audio loop, parameters, gate / fuzz / overdrive / delay |
| `reverb.go` | Freeverb-style reverb |
| `tuner.go` | YIN pitch detection and guitar tunings |
| `server.go`, `index.html` | Web server and UI |

## Ideas / TODO

- Lower latency audio backend
- More effects (chorus, tremolo, wah, cabinet simulation)
- Presets
- Better tuner smoothing

## License

[MIT](LICENSE)
