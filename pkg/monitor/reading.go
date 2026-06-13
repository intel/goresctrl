/*
Copyright 2026 Intel Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package monitor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadingKind distinguishes gauge from cumulative counter semantics.
type ReadingKind int

const (
	// Gauge is an instantaneous point-in-time value (e.g. llc_occupancy).
	Gauge ReadingKind = iota
	// Cumulative is a monotonically increasing counter (e.g. mbm_total_bytes, core_energy).
	Cumulative
)

// maxReadings caps the number of counter readings returned per ReadCounters
// call. Real resctrl hardware exposes O(10) counters × O(10) domains; a much
// larger count indicates a misconfigured or tampered filesystem.
const maxReadings = 4096

// Reading is one raw counter sample from one monitoring domain.
type Reading struct {
	Domain string      // resctrl mon_data subdir, e.g. "mon_L3_00", "mon_PERF_PKG_01"
	Name   string      // counter file name, e.g. "llc_occupancy", "core_energy"
	Value  float64     // parsed value (float to cover core_energy/activity)
	Kind   ReadingKind // Gauge or Cumulative
	Unit   string      // UCUM unit where available (e.g. "By", "J"), otherwise descriptive (e.g. "farads")
}

// readingMeta maps known counter names to their kind and unit.
var readingMeta = map[string]struct {
	kind ReadingKind
	unit string
}{
	"llc_occupancy":        {Gauge, "By"},
	"mbm_total_bytes":      {Cumulative, "By"},
	"mbm_local_bytes":      {Cumulative, "By"},
	"core_energy":          {Cumulative, "J"},
	"activity":             {Cumulative, "farads"},
	"c1_res":               {Cumulative, ""},
	"c6_res":               {Cumulative, ""},
	"uops_retired":         {Cumulative, ""},
	"unhalted_core_cycles": {Cumulative, ""},
	"unhalted_ref_cycles":  {Cumulative, ""},
	"stalls_llc_miss":      {Cumulative, ""},
	"stalls_llc_hit":       {Cumulative, ""},
	"stalls_l2miss":        {Cumulative, ""},
	"stalls_l3miss":        {Cumulative, ""},
	"stalls_memrd":         {Cumulative, ""},
	"stalls_memwr":         {Cumulative, ""},
	"stalls_llcmiss":       {Cumulative, ""},
	"stalls_ldpending":     {Cumulative, ""},
}

// ReadCounters walks <group>/mon_data/<domain>/* for the tracked key and
// returns every readable counter file. Missing files/dirs are skipped, not
// errors: not every domain exposes every counter. Returns ErrNotTracked if the
// key is unknown.
func (m *Manager) ReadCounters(key string) ([]Reading, error) {
	key = m.canonKey(key)

	m.mu.Lock()
	e, ok := m.entries[key]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotTracked
	}
	return readMonData(filepath.Join(e.dir, "mon_data"))
}

func readMonData(monDataPath string) ([]Reading, error) {
	domains, err := os.ReadDir(monDataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // group exists but no mon_data yet
		}
		return nil, err
	}

	var out []Reading
	for _, d := range domains {
		if !d.IsDir() {
			continue
		}
		domain := d.Name()
		domainPath := filepath.Join(monDataPath, domain)
		files, err := os.ReadDir(domainPath)
		if err != nil {
			continue // domain raced away; skip
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(domainPath, f.Name()))
			if err != nil {
				continue // counter file unreadable; skip
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			if err != nil {
				continue // non-numeric (e.g. "Unavailable"); skip
			}
			out = append(out, Reading{
				Domain: domain,
				Name:   f.Name(),
				Value:  v,
				Kind:   metaKind(f.Name()),
				Unit:   metaUnit(f.Name()),
			})
			if len(out) >= maxReadings {
				return out, nil
			}
		}
	}
	return out, nil
}

// metaKind returns the ReadingKind for a known counter name, defaulting to Cumulative.
func metaKind(name string) ReadingKind {
	if m, ok := readingMeta[name]; ok {
		return m.kind
	}
	return Cumulative
}

// metaUnit returns the unit hint for a known counter name, or "" if unknown.
func metaUnit(name string) string {
	if m, ok := readingMeta[name]; ok {
		return m.unit
	}
	return ""
}
