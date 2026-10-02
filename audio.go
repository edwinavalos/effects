package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// Source is a capture device the user can pick in the UI.
type Source struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Guitar      bool   `json:"guitar"`
}

func isGuitar(s string) bool {
	s = strings.ToLower(s)
	if strings.Contains(s, "webcam") {
		return false
	}
	for _, k := range []string{"guitar", "rocksmith", "instrument"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// listSources returns the capture devices PipeWire/PulseAudio currently knows about
// (monitors of output devices are left out).
func listSources() []Source {
	out, err := exec.Command("pactl", "-f", "json", "list", "sources").Output()
	if err != nil {
		return nil
	}
	var raw []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	res := []Source{}
	for _, r := range raw {
		if strings.HasSuffix(r.Name, ".monitor") {
			continue
		}
		res = append(res, Source{r.Name, r.Description, isGuitar(r.Name + " " + r.Description)})
	}
	return res
}

// pickSource resolves the wanted source ("" = auto-detect a guitar interface)
// against the devices that are present. It returns "" if nothing suitable is plugged in.
func pickSource(want string) string {
	for _, s := range listSources() {
		if want != "" && s.Name == want || want == "" && s.Guitar {
			return s.Name
		}
	}
	return ""
}
