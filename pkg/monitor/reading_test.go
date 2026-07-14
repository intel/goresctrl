//go:build linux

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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCounters_MultiDomain(t *testing.T) {
	// Use the static testdata fixtures.
	mgr := &Manager{
		root:     t.TempDir(),
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	// Point the entry's dir at testdata (which contains mon_data/).
	mgr.entries["test-pod"] = &entry{
		dir: "testdata",
	}

	readings, err := mgr.ReadCounters("test-pod")
	require.NoError(t, err)

	// Build a lookup map: domain/name -> value
	type key struct{ domain, name string }
	got := make(map[key]float64)
	for _, r := range readings {
		got[key{r.Domain, r.Name}] = r.Value
	}

	// L3 domain — integer values parse as float64.
	assert.InDelta(t, 540672.0, got[key{"mon_L3_00", "llc_occupancy"}], 0.001)
	assert.InDelta(t, 123456789.0, got[key{"mon_L3_00", "mbm_total_bytes"}], 0.001)

	// PERF_PKG domain — float values.
	assert.InDelta(t, 54446119.644974, got[key{"mon_PERF_PKG_00", "core_energy"}], 0.001)
	assert.InDelta(t, 1042.371582, got[key{"mon_PERF_PKG_00", "activity"}], 0.001)

	// mon_L3_01 has only "Unavailable" — no reading should be emitted for it.
	_, hasL301 := got[key{"mon_L3_01", "llc_occupancy"}]
	assert.False(t, hasL301, "non-numeric 'Unavailable' should be skipped")
}

func TestReadCounters_ErrNotTracked(t *testing.T) {
	mgr := &Manager{
		root:     t.TempDir(),
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	_, err := mgr.ReadCounters("nonexistent")
	assert.True(t, errors.Is(err, ErrNotTracked))
}

func TestReadCounters_MissingMonData(t *testing.T) {
	tmpDir := t.TempDir()

	mgr := &Manager{
		root:     tmpDir,
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	// Entry exists but points to a directory without mon_data/.
	mgr.entries["no-mon-data"] = &entry{
		dir: tmpDir,
	}

	readings, err := mgr.ReadCounters("no-mon-data")
	require.NoError(t, err)
	assert.Empty(t, readings)
}

func TestReadCounters_CanonicalizesKey(t *testing.T) {
	mgr := &Manager{
		root:     t.TempDir(),
		validKey: PodUIDValidator,
		canonKey: CanonicalizePodUID,
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	// CanonicalizePodUID always produces dashed form.
	const dashed = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	mgr.entries[dashed] = &entry{
		dir: "testdata",
	}

	// Call with compact form; should canonicalize to dashed and find the entry.
	const compact = "a1b2c3d4e5f67890abcdef1234567890"
	readings, err := mgr.ReadCounters(compact)
	require.NoError(t, err)
	assert.NotEmpty(t, readings)
}

func TestSnapshot_ReturnsGroupHandles(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := &Manager{
		root:     tmpDir,
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	dir1 := filepath.Join(tmpDir, "mon_groups", "pod-a")
	dir2 := filepath.Join(tmpDir, "mon_groups", "pod-b")
	mgr.entries["pod-a"] = &entry{dir: dir1}
	mgr.entries["pod-b"] = &entry{dir: dir2}

	snap := mgr.Snapshot()

	assert.Len(t, snap, 2)
	assert.Equal(t, "pod-a", snap["pod-a"].Key())
	assert.Equal(t, dir1, snap["pod-a"].Path())
	assert.Equal(t, "pod-b", snap["pod-b"].Key())
	assert.Equal(t, dir2, snap["pod-b"].Path())
}

func TestSnapshot_IsolatedFromManager(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := &Manager{
		root:     tmpDir,
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	dir1 := filepath.Join(tmpDir, "mon_groups", "pod-x")
	mgr.entries["pod-x"] = &entry{dir: dir1}

	snap := mgr.Snapshot()
	assert.Len(t, snap, 1)

	// Mutating the snapshot should not affect the Manager.
	delete(snap, "pod-x")
	assert.Len(t, mgr.Snapshot(), 1, "deleting from snapshot must not affect manager state")
}

func TestSnapshot_Empty(t *testing.T) {
	mgr := &Manager{
		root:     t.TempDir(),
		validKey: DefaultKeyValidator,
		canonKey: func(k string) string { return k },
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		rmdir:    os.Remove,
	}

	snap := mgr.Snapshot()
	assert.Empty(t, snap)
}
