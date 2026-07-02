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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_Defaults(t *testing.T) {
	mgr, err := New(Options{})
	require.NoError(t, err)

	assert.Equal(t, defaultResctrlRoot, mgr.root)
	assert.NotNil(t, mgr.validKey)
	assert.NotNil(t, mgr.entries)
	assert.NotNil(t, mgr.mkdir)
	assert.NotNil(t, mgr.rmdir)

	// Default validator should accept a simple key.
	assert.True(t, mgr.validKey("my-pod-uid"))
}

func TestNew_ExplicitRoot(t *testing.T) {
	mgr, err := New(Options{
		ResctrlRoot: "/tmp/fake-resctrl",
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/fake-resctrl", mgr.root)
}

func TestNew_CustomValidator(t *testing.T) {
	called := false
	custom := func(key string) bool {
		called = true
		return key == "allowed"
	}

	mgr, err := New(Options{
		KeyValidator: custom,
	})
	require.NoError(t, err)

	// Should use our custom validator, not the default.
	assert.True(t, mgr.validKey("allowed"))
	assert.True(t, called)
	assert.False(t, mgr.validKey("rejected"))
}

func TestEnsureGroup_CanonicalizesKey(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{
		ResctrlRoot:      tmpDir,
		KeyValidator:     PodUIDValidator,
		KeyCanonicalizer: CanonicalizePodUID,
	})
	require.NoError(t, err)

	const compact = "a1b2c3d4e5f67890abcdef1234567890"
	const dashed = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	// A compact (dashless) pod UID is accepted and creates a dashed directory.
	grp, err := mgr.EnsureGroup(compact, "")
	require.NoError(t, err)
	assert.Equal(t, dashed, grp.Key())
	assert.Equal(t, filepath.Join(tmpDir, "mon_groups", dashed), grp.Path())

	info, err := os.Stat(grp.Path())
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// The dashed form refers to the same group (idempotent, no second dir).
	grp2, err := mgr.EnsureGroup(dashed, "")
	require.NoError(t, err)
	assert.Equal(t, grp.Path(), grp2.Path())
	assert.Len(t, mgr.List(), 1)

	// Removal works via either form.
	require.NoError(t, mgr.Remove(compact))
	assert.Empty(t, mgr.List())
}

func TestNew_DefaultValidatorRejectsUnsafe(t *testing.T) {
	mgr, err := New(Options{})
	require.NoError(t, err)

	tests := []struct {
		name string
		key  string
		ok   bool
	}{
		{"simple", "pod-uid-1", true},
		{"uuid", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", true},
		{"empty", "", false},
		{"dot", ".", false},
		{"dotdot", "..", false},
		{"slash", "foo/bar", false},
		{"nul", "foo\x00bar", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.ok, mgr.validKey(tc.key))
		})
	}
}

func TestNew_EntriesMapInitialized(t *testing.T) {
	mgr, err := New(Options{})
	require.NoError(t, err)

	// List on fresh manager should be empty, not nil.
	keys := mgr.List()
	assert.NotNil(t, keys)
	assert.Empty(t, keys)
}

// --- EnsureGroup tests (Task 2.2) ---

func TestEnsureGroup_RootClass(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)
	assert.Equal(t, "pod-uid-1", grp.Key())
	assert.Equal(t, filepath.Join(tmpDir, "mon_groups", "pod-uid-1"), grp.Path())

	// Directory should exist on disk.
	info, err := os.Stat(grp.Path())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestEnsureGroup_WithRDTClass(t *testing.T) {
	tmpDir := t.TempDir()
	// Create the ctrl_group that must pre-exist.
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "BestEffort"), 0755))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-2", "BestEffort")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpDir, "BestEffort", "mon_groups", "pod-uid-2"), grp.Path())

	info, err := os.Stat(grp.Path())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestEnsureGroup_MissingCtrlGroup(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// Attempt to create under a non-existent ctrl_group.
	_, err = mgr.EnsureGroup("pod-uid-3", "NoSuchClass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ctrl_group")

	// Verify the ctrl_group was NOT created.
	_, statErr := os.Stat(filepath.Join(tmpDir, "NoSuchClass"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestEnsureGroup_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp1, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	grp2, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	assert.Equal(t, grp1.Path(), grp2.Path())

	// Should only be one entry tracked.
	assert.Equal(t, 1, len(mgr.List()))
}

func TestEnsureGroup_RecreatesAfterOutOfBandRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	// Simulate out-of-band removal (admin or ctrl_group deletion).
	require.NoError(t, os.Remove(grp.Path()))

	// EnsureGroup should detect the missing directory and recreate it.
	grp2, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)
	assert.Equal(t, grp.Path(), grp2.Path())

	// The directory must exist on disk.
	info, err := os.Stat(grp2.Path())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestEnsureGroup_BadKey(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("", "")
	assert.ErrorIs(t, err, ErrBadKey)

	_, err = mgr.EnsureGroup("foo/bar", "")
	assert.ErrorIs(t, err, ErrBadKey)
}

func TestEnsureGroup_BadClass(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("pod-uid-1", "../escape")
	assert.ErrorIs(t, err, ErrBadClass)

	_, err = mgr.EnsureGroup("pod-uid-1", ".")
	assert.ErrorIs(t, err, ErrBadClass)
}

func TestEnsureGroup_ENOSPC(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// Inject a mkdir that returns ENOSPC (simulating RMID exhaustion).
	mgr.mkdir = func(name string, perm os.FileMode) error {
		return &os.PathError{Op: "mkdir", Path: name, Err: syscall.ENOSPC}
	}

	_, err = mgr.EnsureGroup("pod-uid-1", "")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoRMIDs))
}

func TestEnsureGroup_AdoptsExistingDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-create the mon_group directory (simulates leftover from previous run).
	monGroupsPath := filepath.Join(tmpDir, "mon_groups")
	require.NoError(t, os.MkdirAll(filepath.Join(monGroupsPath, "pod-uid-1"), 0755))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// Should succeed (adopt existing) without error.
	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(monGroupsPath, "pod-uid-1"), grp.Path())
	assert.Equal(t, 1, len(mgr.List()))
}

// --- AssignPID tests (Task 2.3) ---

func TestAssignPID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	// Simulate kernel creating the tasks file when mon_group dir is created.
	tasksPath := filepath.Join(grp.Path(), "tasks")
	require.NoError(t, os.WriteFile(tasksPath, nil, 0644))

	err = mgr.AssignPID("pod-uid-1", 12345)
	require.NoError(t, err)

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, "12345\n", string(data))
}

func TestAssignPID_UntrackedKey(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.AssignPID("no-such-key", 999)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotTracked))
}

func TestAssignPID_MultipleWrites(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	tasksPath := filepath.Join(grp.Path(), "tasks")
	require.NoError(t, os.WriteFile(tasksPath, nil, 0644))

	// Assign two PIDs sequentially. On real resctrl the kernel handles each
	// write independently. On a regular file the second write overwrites
	// the first (no O_APPEND). Verify the last write succeeded.
	require.NoError(t, mgr.AssignPID("pod-uid-1", 100))
	require.NoError(t, mgr.AssignPID("pod-uid-1", 200))

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "200\n")
}

// TestAssignPID_RefusesCrossClass verifies that assigning a PID that already
// belongs to a different non-root ctrl_group is refused with ErrClassMismatch,
// so an off-class sidecar's allocation is not silently clobbered.
func TestAssignPID_RefusesCrossClass(t *testing.T) {
	tmpDir := t.TempDir()
	// Two ctrl_groups pre-exist.
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "ClassA"), 0755))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "ClassB"), 0755))
	// The PID is already allocated to ClassB.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ClassB", "tasks"), []byte("777\n"), 0644))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// The pod's mon_group lives under ClassA.
	grp, err := mgr.EnsureGroup("pod-uid-1", "ClassA")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(grp.Path(), "tasks"), nil, 0644))

	err = mgr.AssignPID("pod-uid-1", 777)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrClassMismatch))

	// The mon_group tasks file must remain empty (no clobbering write).
	data, err := os.ReadFile(filepath.Join(grp.Path(), "tasks"))
	require.NoError(t, err)
	assert.Empty(t, string(data))
}

// TestAssignPID_AllowsSameClass verifies that a PID already in the group's own
// ctrl_group is assigned normally (idempotent re-attribution within a class).
func TestAssignPID_AllowsSameClass(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "ClassA"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ClassA", "tasks"), []byte("888\n"), 0644))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "ClassA")
	require.NoError(t, err)
	tasksPath := filepath.Join(grp.Path(), "tasks")
	require.NoError(t, os.WriteFile(tasksPath, nil, 0644))

	require.NoError(t, mgr.AssignPID("pod-uid-1", 888))

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, "888\n", string(data))
}

// TestAssignPID_AllowsFromRoot verifies that a PID not in any non-root
// ctrl_group (i.e. still in the root group) is placed into the group's class.
func TestAssignPID_AllowsFromRoot(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "ClassA"), 0755))
	// ClassA has no tasks file / does not contain the PID.

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "ClassA")
	require.NoError(t, err)
	assert.Equal(t, "ClassA", grp.Class())
	tasksPath := filepath.Join(grp.Path(), "tasks")
	require.NoError(t, os.WriteFile(tasksPath, nil, 0644))

	require.NoError(t, mgr.AssignPID("pod-uid-1", 999))

	data, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Equal(t, "999\n", string(data))
}

// TestAssignPID_RefusesRootGroupClobber verifies that a PID allocated to a
// non-root class cannot be dragged into a root-scoped mon_group (which would
// reset its CLOSID to the root/default allocation).
func TestAssignPID_RefusesRootGroupClobber(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "ClassB"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "ClassB", "tasks"), []byte("555\n"), 0644))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// Root-scoped mon_group (rdtClass == "").
	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(grp.Path(), "tasks"), nil, 0644))

	err = mgr.AssignPID("pod-uid-1", 555)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrClassMismatch))
}

// --- Remove tests (Task 2.4) ---

func TestRemove(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)
	dir := grp.Path()

	// Directory should exist.
	_, err = os.Stat(dir)
	require.NoError(t, err)

	err = mgr.Remove("pod-uid-1")
	require.NoError(t, err)

	// Directory should be gone.
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err))

	// Key should be untracked.
	assert.Empty(t, mgr.List())
}

func TestRemove_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	// Remove the directory out-of-band (simulates kernel removed it or race).
	require.NoError(t, os.Remove(grp.Path()))

	// Remove should still succeed (tolerates ErrNotExist).
	err = mgr.Remove("pod-uid-1")
	assert.NoError(t, err)
	assert.Empty(t, mgr.List())
}

func TestRemove_UntrackedKey(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Remove("no-such-key")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotTracked))
}

// --- Reconcile tests (Task 3.1) ---

// helper: create a fake resctrl tree with mon_groups directories.
func setupReconcileTree(t *testing.T, root string, dirs map[string][]string) {
	t.Helper()
	for parent, groups := range dirs {
		var monDir string
		if parent == "." {
			monDir = filepath.Join(root, "mon_groups")
		} else {
			monDir = filepath.Join(root, parent, "mon_groups")
		}
		require.NoError(t, os.MkdirAll(monDir, 0755))
		for _, g := range groups {
			require.NoError(t, os.Mkdir(filepath.Join(monDir, g), 0755))
		}
	}
}

func TestReconcile_ReapsOwnOrphan(t *testing.T) {
	tmpDir := t.TempDir()
	setupReconcileTree(t, tmpDir, map[string][]string{
		".": {"dead-pod"},
	})

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Reconcile(nil)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", "dead-pod"))
	assert.True(t, os.IsNotExist(err))
}

func TestReconcile_KeepsLiveKey(t *testing.T) {
	tmpDir := t.TempDir()
	setupReconcileTree(t, tmpDir, map[string][]string{
		".": {"live-pod", "dead-pod"},
	})

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Reconcile([]string{"live-pod"})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", "live-pod"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", "dead-pod"))
	assert.True(t, os.IsNotExist(err))
}

func TestReconcile_KeyValidatorScopesReaping(t *testing.T) {
	tmpDir := t.TempDir()
	validUUID := "11111111-2222-3333-4444-555555555555"
	setupReconcileTree(t, tmpDir, map[string][]string{
		".": {"not-a-uuid", validUUID},
	})

	// PodUIDValidator scopes reaping to UUID-shaped directories only.
	mgr, err := New(Options{ResctrlRoot: tmpDir, KeyValidator: PodUIDValidator})
	require.NoError(t, err)

	err = mgr.Reconcile(nil)
	require.NoError(t, err)

	// Non-UUID directory is left untouched (not owned by this mechanism).
	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", "not-a-uuid"))
	assert.NoError(t, err)
	// UUID-shaped orphan is reaped.
	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", validUUID))
	assert.True(t, os.IsNotExist(err))
}

func TestReconcile_IgnoresNonKeyDirs(t *testing.T) {
	tmpDir := t.TempDir()
	monDir := filepath.Join(tmpDir, "mon_groups")
	require.NoError(t, os.MkdirAll(monDir, 0755))
	// Kernel-managed dir that should never be removed.
	require.NoError(t, os.Mkdir(filepath.Join(monDir, "info"), 0755))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Reconcile(nil)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(monDir, "info"))
	assert.NoError(t, err, "should not remove info")
}

func TestReconcile_CtrlGroupLevel(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "BestEffort"), 0755))
	setupReconcileTree(t, tmpDir, map[string][]string{
		"BestEffort": {"dead", "alive"},
	})

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Reconcile([]string{"alive"})
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpDir, "BestEffort", "mon_groups", "alive"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmpDir, "BestEffort", "mon_groups", "dead"))
	assert.True(t, os.IsNotExist(err))
}

func TestReconcile_SkipsResctrlSpecialDirs(t *testing.T) {
	tmpDir := t.TempDir()
	for _, d := range []string{"info", "mon_groups", "mon_L3_00"} {
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, d), 0755))
	}

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	err = mgr.Reconcile(nil)
	assert.NoError(t, err)
}

func TestReconcile_ReapsStaleDuplicate(t *testing.T) {
	// Scenario: key is tracked under BestEffort (via EnsureGroup), but a stale
	// duplicate directory exists at root level (left over from a class change).
	// Tasks-exclusivity guarantees the PIDs reside at the authoritative path,
	// so the duplicate should be reaped even though the key is "live".
	tmpDir := t.TempDir()

	// Create ctrl_group directory so EnsureGroup can place the group there.
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "BestEffort", "mon_groups"), 0755))
	// Also create root-level mon_groups with the stale duplicate.
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "mon_groups", "pod-uid-1"), 0755))

	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	// Track the key under BestEffort — this is the authoritative location.
	_, err = mgr.EnsureGroup("pod-uid-1", "BestEffort")
	require.NoError(t, err)

	// Reconcile with the key declared live.
	err = mgr.Reconcile([]string{"pod-uid-1"})
	require.NoError(t, err)

	// The authoritative directory must still exist.
	_, err = os.Stat(filepath.Join(tmpDir, "BestEffort", "mon_groups", "pod-uid-1"))
	assert.NoError(t, err, "authoritative directory must be kept")

	// The stale duplicate at root level must be reaped.
	_, err = os.Stat(filepath.Join(tmpDir, "mon_groups", "pod-uid-1"))
	assert.True(t, os.IsNotExist(err), "stale duplicate must be reaped")
}

// --- Security tests ---

func TestEnsureGroup_CanonicalizedKeyPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	// A malicious canonicalizer that tries to escape via path traversal.
	mgr, err := New(Options{
		ResctrlRoot:      tmpDir,
		KeyCanonicalizer: func(key string) string { return "../../etc/" + key },
	})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("passwd", "")
	assert.ErrorIs(t, err, ErrBadKey)
	assert.Contains(t, err.Error(), "not path-safe")
}

func TestEnsureGroup_CanonicalizedDotDot(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{
		ResctrlRoot:      tmpDir,
		KeyCanonicalizer: func(key string) string { return ".." },
	})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("anything", "")
	assert.ErrorIs(t, err, ErrBadKey)
}

func TestAssignPID_RejectsInvalidPID(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	err = mgr.AssignPID("pod-uid-1", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pid")

	err = mgr.AssignPID("pod-uid-1", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pid")
}

func TestRemove_KeepsEntryOnRmdirFailure(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)

	_, err = mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	// Inject an rmdir that always fails.
	mgr.rmdir = func(string) error {
		return fmt.Errorf("injected rmdir failure")
	}

	err = mgr.Remove("pod-uid-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "injected rmdir failure")

	// Key should still be tracked (entry left intact, retryable).
	assert.Equal(t, 1, len(mgr.List()))
}

// TestRemove_ConcurrentEnsureGroup exercises the race where Remove and
// EnsureGroup run concurrently for the same key. The Manager must never end up
// with map state that contradicts what EnsureGroup returned: if EnsureGroup
// returns a Group, that key must be tracked afterwards.
func TestRemove_ConcurrentEnsureGroup(t *testing.T) {
	iters := 200
	if testing.Short() {
		iters = 10
	}
	for iter := 0; iter < iters; iter++ {
		tmpDir := t.TempDir()
		mgr, err := New(Options{ResctrlRoot: tmpDir})
		require.NoError(t, err)

		_, err = mgr.EnsureGroup("pod-uid-1", "")
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			mgr.Remove("pod-uid-1")
		}()
		var ensureErr error
		go func() {
			defer wg.Done()
			_, ensureErr = mgr.EnsureGroup("pod-uid-1", "")
		}()
		wg.Wait()

		// If EnsureGroup succeeded and the key is tracked, the directory it
		// returned must exist on disk. There must be no torn state where the
		// map tracks an entry whose directory was reaped by the concurrent
		// Remove.
		if ensureErr == nil && len(mgr.List()) == 1 {
			dir := filepath.Join(tmpDir, "mon_groups", "pod-uid-1")
			_, statErr := os.Stat(dir)
			assert.NoError(t, statErr, "tracked entry must have its directory on disk")
		}
	}
}

func TestRemove_ConcurrentReadCounters(t *testing.T) {
	tmpDir := t.TempDir()
	mgr, err := New(Options{ResctrlRoot: tmpDir})
	require.NoError(t, err)
	// Use RemoveAll to model resctrl's rmdir which removes the entire
	// mon_group tree (including mon_data subdirectories) atomically.
	mgr.rmdir = os.RemoveAll

	grp, err := mgr.EnsureGroup("pod-uid-1", "")
	require.NoError(t, err)

	// Create a mon_data tree so ReadCounters has something to read.
	monData := filepath.Join(grp.Path(), "mon_data", "mon_L3_00")
	require.NoError(t, os.MkdirAll(monData, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(monData, "llc_occupancy"), []byte("42\n"), 0644))

	// Run Remove and ReadCounters concurrently. Neither should panic or
	// trigger a data race.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			mgr.ReadCounters("pod-uid-1")
		}
	}()
	// Give the reader goroutine a head start, then remove.
	mgr.Remove("pod-uid-1")
	<-done
}
