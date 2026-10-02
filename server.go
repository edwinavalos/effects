package main

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
)

//go:embed index.html
var indexHTML []byte

func serve(e *Engine, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	// GET: params + live level + tuner reading. POST: partial params update.
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var err error
			e.update(func(p *Params) { err = json.NewDecoder(r.Body).Decode(p) })
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		e.mu.Lock()
		state := struct {
			Params   Params    `json:"params"`
			Level    float64   `json:"level"`
			Tuner    Reading   `json:"tuner"`
			Spectrum []float64 `json:"spectrum"`
			Source   struct {
				Want   string `json:"want"`
				Active string `json:"active"`
			} `json:"source"`
		}{Params: e.p, Level: e.level, Tuner: e.tuner}
		state.Source.Want, state.Source.Active = e.want, e.active
		state.Spectrum = append([]float64(nil), e.spectrum...)
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	})
	// GET: capture devices currently present. POST {"name": "..."}: pick one ("" = auto).
	mux.HandleFunc("/api/sources", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			e.setSource(body.Name)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(listSources())
	})
	log.Fatal(http.ListenAndServe(addr, mux))
}
