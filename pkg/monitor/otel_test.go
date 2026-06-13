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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestInstrumentName(t *testing.T) {
	tests := []struct {
		domain, counter, want string
	}{
		{"mon_L3_00", "llc_occupancy", "l3.llc.occupancy"},
		{"mon_L3_00", "mbm_local_bytes", "l3.mbm.local.bytes"},
		{"mon_L3_00", "mbm_total_bytes", "l3.mbm.total.bytes"},
		{"mon_L3_01", "llc_occupancy", "l3.llc.occupancy"},
		{"mon_PERF_PKG_00", "core_energy", "perf.core.energy"},
		{"mon_PERF_PKG_00", "activity", "perf.activity"},
		{"mon_PERF_PKG_01", "c1_res", "perf.c1.res"},
		{"mon_PERF_PKG_00", "stalls_llcmiss", "perf.stalls.llcmiss"},
		{"mon_PERF_PKG_00", "unhalted_core_cycles", "perf.unhalted.core.cycles"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := InstrumentName(tt.domain, tt.counter)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDomainPrefix(t *testing.T) {
	tests := []struct {
		domain, want string
	}{
		{"mon_L3_00", "l3"},
		{"mon_L3_01", "l3"},
		{"mon_PERF_PKG_00", "perf"},
		{"mon_PERF_PKG_01", "perf"},
		{"mon_MBA_00", "mba"},
		{"L3_00", "l3"},        // no mon_ prefix
		{"unknown", "unknown"}, // no parseable suffix
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			assert.Equal(t, tt.want, DomainPrefix(tt.domain))
		})
	}
}

func TestDomainInstance(t *testing.T) {
	tests := []struct {
		domain, want string
	}{
		{"mon_L3_00", "00"},
		{"mon_L3_01", "01"},
		{"mon_PERF_PKG_00", "00"},
		{"mon_PERF_PKG_01", "01"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			assert.Equal(t, tt.want, DomainInstance(tt.domain))
		})
	}
}

func TestOTelAccumulator_Monotonic(t *testing.T) {
	a := newOTelAccumulator()

	// First value initializes.
	v := a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 100)
	assert.Equal(t, 100.0, v)

	// Positive delta accumulates.
	v = a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 150)
	assert.Equal(t, 150.0, v)

	// Negative delta (hardware race) is suppressed.
	v = a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 148)
	assert.Equal(t, 150.0, v) // unchanged

	// Recovery after negative: only the positive delta from 148→200 counts.
	v = a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 200)
	assert.Equal(t, 202.0, v) // 150 + (200-148)
}

func TestOTelAccumulator_PruneStaleGroups(t *testing.T) {
	a := newOTelAccumulator()

	a.monotonic("pod1", "mon_L3_00", "llc_occupancy", 42)
	a.monotonic("pod2", "mon_L3_00", "llc_occupancy", 99)

	// Prune with only pod1 live — pod2 state should be removed.
	a.pruneStaleGroups(map[string]struct{}{"pod1": {}})

	a.mu.Lock()
	_, hasPod1 := a.state["pod1\x00mon_L3_00\x00llc_occupancy"]
	_, hasPod2 := a.state["pod2\x00mon_L3_00\x00llc_occupancy"]
	a.mu.Unlock()
	assert.True(t, hasPod1)
	assert.False(t, hasPod2)
}

func TestOTelAccumulator_FilteredCounterRetainsState(t *testing.T) {
	a := newOTelAccumulator()

	// Accumulate a value.
	v := a.monotonic("pod1", "mon_PERF_PKG_00", "core_energy", 100)
	assert.Equal(t, 100.0, v)

	// Simulate a cycle where pod1 is still alive but the counter is
	// filtered/temporarily unreadable (monotonic not called).
	a.pruneStaleGroups(map[string]struct{}{"pod1": {}})

	// Counter comes back — accumulated value should continue from where
	// it left off, not reset.
	v = a.monotonic("pod1", "mon_PERF_PKG_00", "core_energy", 150)
	assert.Equal(t, 150.0, v)
}

func TestOTelAccumulator_IndependentSeries(t *testing.T) {
	a := newOTelAccumulator()

	// Two different groups don't interfere.
	v1 := a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 100)
	v2 := a.monotonic("pod2", "mon_L3_00", "mbm_total_bytes", 200)
	assert.Equal(t, 100.0, v1)
	assert.Equal(t, 200.0, v2)

	v1 = a.monotonic("pod1", "mon_L3_00", "mbm_total_bytes", 120)
	v2 = a.monotonic("pod2", "mon_L3_00", "mbm_total_bytes", 250)
	assert.Equal(t, 120.0, v1)
	assert.Equal(t, 250.0, v2)
}

func TestRegisterOTelInstruments_NoMonData(t *testing.T) {
	// A Manager with a root that has no mon_data should return no instruments
	// from discoverAndRegister (discoverCounters returns nil for missing dir).
	dir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: dir})
	require.NoError(t, err)

	meter := noop.Meter{}
	reg, err := mgr.RegisterOTelInstruments(meter)
	// discoverCounters returns nil for non-existent mon_data, so no instruments
	// are registered — that's fine, not an error.
	require.NoError(t, err)
	require.NotNil(t, reg)
}

func TestRegisterOTelInstruments_WithCounters(t *testing.T) {
	dir := t.TempDir()
	// Create synthetic mon_data tree.
	l3Dir := dir + "/mon_data/mon_L3_00"
	perfDir := dir + "/mon_data/mon_PERF_PKG_00"
	require.NoError(t, os.MkdirAll(l3Dir, 0755))
	require.NoError(t, os.MkdirAll(perfDir, 0755))
	require.NoError(t, os.WriteFile(l3Dir+"/llc_occupancy", []byte("1024\n"), 0644))
	require.NoError(t, os.WriteFile(l3Dir+"/mbm_local_bytes", []byte("4096\n"), 0644))
	require.NoError(t, os.WriteFile(perfDir+"/core_energy", []byte("99.5\n"), 0644))

	mgr, err := New(Options{ResctrlRoot: dir})
	require.NoError(t, err)

	meter := noop.Meter{}
	reg, err := mgr.RegisterOTelInstruments(meter)
	require.NoError(t, err)
	require.NotNil(t, reg)
}

func TestRegisterOTelInstruments_UnavailableCounterDiscovered(t *testing.T) {
	dir := t.TempDir()
	l3Dir := dir + "/mon_data/mon_L3_00"
	require.NoError(t, os.MkdirAll(l3Dir, 0755))
	// Counter file with non-numeric value; should still be discovered.
	require.NoError(t, os.WriteFile(l3Dir+"/llc_occupancy", []byte("Unavailable\n"), 0644))

	mgr, err := New(Options{ResctrlRoot: dir})
	require.NoError(t, err)

	meter := noop.Meter{}
	reg, err := mgr.RegisterOTelInstruments(meter)
	require.NoError(t, err)
	require.NotNil(t, reg)
}
