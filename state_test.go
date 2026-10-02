package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	if got := loadParams(path); !reflect.DeepEqual(got, defaultParams()) {
		t.Fatal("missing file should give defaults")
	}
	p := defaultParams()
	p.Mode = "tuner"
	p.Dist = 55
	p.Order = []string{"dist", "bogus", "reverb"}
	if err := saveParams(path, p); err != nil {
		t.Fatal(err)
	}
	got := loadParams(path)
	if got.Dist != 55 || got.Mode != "pedal" || !reflect.DeepEqual(got.Order, []string{"dist", "reverb"}) {
		t.Fatalf("unexpected params: %+v", got)
	}
}
