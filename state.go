package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// defaultStatePath is where the pedalboard is remembered between runs.
func defaultStatePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "effects-state.json"
	}
	return filepath.Join(dir, "effects", "state.json")
}

// loadParams reads saved params from path on top of the defaults, so fields added
// in newer versions keep sensible values. A missing or broken file gives the defaults.
func loadParams(path string) Params {
	p := defaultParams()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Println("state:", err)
		}
		return p
	}
	if err := json.Unmarshal(data, &p); err != nil {
		log.Println("state: ignoring unreadable", path+":", err)
		return defaultParams()
	}
	p.Mode = "pedal" // never start muted in tuner mode
	p.Order = slices.DeleteFunc(p.Order, func(id string) bool { return !slices.Contains(effectIDs, id) })
	p.sanitize()
	return p
}

// saveParams writes p to path atomically.
func saveParams(path string, p Params) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// autosave writes the params to path shortly after they stop changing (knobs
// fire many updates per second while being dragged).
func (e *Engine) autosave(path string) {
	e.saveCh = make(chan struct{}, 1)
	go func() {
		for range e.saveCh {
			for settled := false; !settled; {
				select {
				case <-e.saveCh:
				case <-time.After(500 * time.Millisecond):
					settled = true
				}
			}
			if err := saveParams(path, e.params()); err != nil {
				log.Println("state: save failed:", err)
			}
		}
	}()
}
