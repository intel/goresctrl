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
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

const (
	defaultResctrlRoot = "/sys/fs/resctrl"
	monGroupsDir       = "mon_groups"
	tasksFile          = "tasks"
)

// Typed errors returned by Manager methods.
var (
	// ErrNotTracked is returned when an operation references a key that has
	// no active mon_group.
	ErrNotTracked = errors.New("monitor: key not tracked")

	// ErrNoRMIDs is returned when the kernel has no available RMIDs (mkdir
	// returns ENOSPC on the resctrl filesystem).
	ErrNoRMIDs = errors.New("monitor: no RMIDs available")

	// ErrBadKey is returned when a key fails the configured KeyValidator.
	ErrBadKey = errors.New("monitor: invalid key")

	// ErrBadClass is returned when an rdtClass name is unsafe (path
	// traversal, empty, or contains separators).
	ErrBadClass = errors.New("monitor: invalid rdt class")

	// ErrClassMismatch is returned by AssignPID when the target PID already
	// belongs to a non-root control group that differs from the mon_group's
	// parent ctrl_group. Writing the PID would silently overwrite its CLOSID
	// (its CAT/MBA allocation), so the assignment is refused instead.
	ErrClassMismatch = errors.New("monitor: pid belongs to a different control group")
)

var log *slog.Logger = slog.Default()

// SetLogger sets the logger used by the package. Safe to call before New.
// A nil argument resets to the default logger.
func SetLogger(l *slog.Logger) {
	if l == nil {
		l = slog.Default()
	}
	log = l
}

// Options configures a Manager.
type Options struct {
	// ResctrlRoot is the resctrl mount point. Default: "/sys/fs/resctrl".
	// Tests typically set this to a temp directory.
	ResctrlRoot string

	// KeyValidator, if set, rejects keys that do not satisfy it (e.g. the
	// pod-UID UUID shape). Default: accept any non-empty key without path
	// separators or NUL bytes.
	//
	// The validator also scopes Reconcile: only on-disk mon_group directories
	// whose name satisfies KeyValidator are eligible for orphan removal.
	// Directories that do not match the validator are unconditionally skipped,
	// so a narrow validator (e.g. PodUIDValidator) limits the blast radius of
	// reconciliation to the set of directory names it recognizes. Directories
	// that DO match but are not in the live set will still be removed.
	KeyValidator func(key string) bool

	// KeyCanonicalizer, if set, normalizes a caller key to a canonical form
	// before it is used as a mon_group directory name and as the in-memory
	// tracking key. It is applied after KeyValidator. Default: identity.
	//
	// Pair it with a matching KeyValidator (e.g. CanonicalizePodUID alongside
	// PodUIDValidator) so that keys reported in different-but-equivalent forms
	// (e.g. a pod UID with or without dashes) map to a single, predictable
	// on-disk directory name.
	KeyCanonicalizer func(key string) string
}

// Manager owns the lifecycle of per-workload resctrl mon_groups.
//
// It is safe for concurrent use from multiple goroutines.
type Manager struct {
	root     string
	validKey func(string) bool
	canonKey func(string) string

	mu      sync.Mutex
	entries map[string]*entry // keyed by canonicalized key (e.g. dashed pod UID)

	// Injectable filesystem operations for unit tests.
	mkdir    func(string, os.FileMode) error
	mkdirAll func(string, os.FileMode) error
	rmdir    func(string) error
}

// entry is the in-memory record for one tracked key.
type entry struct {
	dir      string // absolute mon_group directory path
	rdtClass string // rdtClass used when group was created
}

// Group is a handle to one mon_group on the resctrl filesystem.
type Group struct {
	key   string
	dir   string
	class string
}

// Key returns the canonicalized tracking key (e.g. dashed pod UID) for this group.
func (g *Group) Key() string { return g.key }

// Path returns the absolute filesystem path of the mon_group directory.
func (g *Group) Path() string { return g.dir }

// Class returns the rdtClass (ctrl_group) the mon_group lives under. An empty
// string means the mon_group is under the root resctrl group.
func (g *Group) Class() string { return g.class }

// New creates a Manager with the given options.
func New(o Options) (*Manager, error) {
	root := o.ResctrlRoot
	if root == "" {
		root = defaultResctrlRoot
	}
	// Ensure root is absolute so that entry.dir and Group.Path() always
	// return absolute paths, regardless of what the caller passed in.
	var err error
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ResctrlRoot %q: %w", o.ResctrlRoot, err)
	}
	valid := o.KeyValidator
	if valid == nil {
		valid = DefaultKeyValidator
	}
	canon := o.KeyCanonicalizer
	if canon == nil {
		canon = func(k string) string { return k }
	}
	return &Manager{
		root:     root,
		validKey: valid,
		canonKey: canon,
		entries:  make(map[string]*entry),
		mkdir:    os.Mkdir,
		mkdirAll: os.MkdirAll,
		rmdir:    os.Remove,
	}, nil
}

// EnsureGroup idempotently creates a mon_group for key under an optional
// pre-existing rdtClass ctrl_group ("" = root resctrl directory). It never
// creates the ctrl_group itself. Returns a Group handle on success.
func (m *Manager) EnsureGroup(key, rdtClass string) (*Group, error) {
	if !m.validKey(key) {
		return nil, fmt.Errorf("%w: %q", ErrBadKey, key)
	}
	if rdtClass != "" && !isValidRDTClass(rdtClass) {
		return nil, fmt.Errorf("%w: %q", ErrBadClass, rdtClass)
	}
	key = m.canonKey(key)

	// Defense-in-depth: verify the canonicalized key is still a safe single
	// path component. A buggy KeyCanonicalizer could introduce slashes or
	// dot-segments that would escape the mon_groups directory.
	if !DefaultKeyValidator(key) {
		return nil, fmt.Errorf("%w: canonicalized %q is not path-safe", ErrBadKey, key)
	}
	// Ensure the canonicalized key still satisfies the configured validator
	// so that Reconcile (which gates orphan removal by KeyValidator) will
	// recognize the resulting on-disk directory as owned.
	if !m.validKey(key) {
		return nil, fmt.Errorf("%w: canonicalized %q does not satisfy KeyValidator", ErrBadKey, key)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Fast path: already tracked — return existing group if rdtClass matches.
	if e, ok := m.entries[key]; ok {
		if rdtClass != e.rdtClass {
			return nil, fmt.Errorf("key %q already tracked under rdtClass %q, cannot reassign to %q", key, e.rdtClass, rdtClass)
		}
		// Verify the on-disk directory still exists. If it was removed
		// out-of-band (e.g. ctrl_group deleted, admin intervention), fall
		// through to the create path to recreate it. Only treat ENOENT as
		// "gone"; for transient errors (EACCES, etc.) return the error and
		// keep the entry intact.
		info, err := os.Stat(e.dir)
		switch {
		case err == nil && info.IsDir():
			return &Group{key: key, dir: e.dir, class: e.rdtClass}, nil
		case err == nil && !info.IsDir():
			return nil, fmt.Errorf("tracked mon_group path %s exists but is not a directory", e.dir)
		case err != nil && !errors.Is(err, os.ErrNotExist):
			return nil, fmt.Errorf("failed to stat tracked mon_group %s: %w", e.dir, err)
		}
		// Directory gone — drop the stale entry so the create path below
		// can rebuild it (or error clearly).
		delete(m.entries, key)
	}

	// Determine parent directory.
	parentDir := m.root
	if rdtClass != "" {
		parentDir = filepath.Join(m.root, rdtClass)
		// The ctrl_group must already exist (created by an allocation plugin).
		info, err := os.Stat(parentDir)
		if err != nil {
			return nil, fmt.Errorf("ctrl_group %s does not exist: %w", parentDir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("ctrl_group %s is not a directory", parentDir)
		}
	}

	monGroupsPath := filepath.Join(parentDir, monGroupsDir)
	monGroupDir := filepath.Join(monGroupsPath, key)

	// Ensure the mon_groups/ directory exists. On a real resctrl mount this
	// is always present; for testing we create it if needed.
	if err := m.mkdirAll(monGroupsPath, 0755); err != nil {
		return nil, fmt.Errorf("mon_groups dir not available at %s: %w", monGroupsPath, err)
	}

	// Use Mkdir (not MkdirAll) for the final mon_group directory to avoid
	// accidentally creating a ctrl_group if rdtClass is wrong.
	if err := m.mkdir(monGroupDir, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			// Already on disk (e.g. from a previous run) — adopt it.
			// Verify it's actually a directory (should always be true on
			// a real resctrl mount, but guards against misconfigured roots).
			if info, serr := os.Stat(monGroupDir); serr != nil {
				return nil, fmt.Errorf("failed to stat existing mon_group %s: %w", monGroupDir, serr)
			} else if !info.IsDir() {
				return nil, fmt.Errorf("mon_group path %s exists but is not a directory", monGroupDir)
			}
			log.Info("adopting existing mon_group", "key", key, "dir", monGroupDir)
		} else if errors.Is(err, syscall.ENOSPC) {
			return nil, fmt.Errorf("%w (key %s): %w", ErrNoRMIDs, key, err)
		} else {
			return nil, fmt.Errorf("failed to create mon_group %s: %w", monGroupDir, err)
		}
	} else {
		log.Info("created mon_group", "key", key, "dir", monGroupDir)
	}

	m.entries[key] = &entry{
		dir:      monGroupDir,
		rdtClass: rdtClass,
	}
	return &Group{key: key, dir: monGroupDir, class: rdtClass}, nil
}

// AssignPID writes pid to the group's tasks file. The kernel assigns the RMID
// to this PID and all future child processes. Call while the init process is
// created but paused (the pre-fork window) for race-free attribution.
func (m *Manager) AssignPID(key string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d: must be positive", pid)
	}
	key = m.canonKey(key)
	m.mu.Lock()
	e, ok := m.entries[key]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotTracked, key)
	}

	// Guard against silently clobbering an existing CAT/MBA allocation.
	//
	// Writing a PID into a mon_group's tasks file moves that task into the
	// mon_group's *parent* ctrl_group, overwriting the task's CLOSID. In a
	// pod-scoped model a pod's mon_group lives under a single ctrl_group, but a
	// pod may contain containers in different RDT classes (e.g. an application
	// and a sidecar). Assigning an off-class container's PID here would
	// silently move it out of its own allocation class. Detect that case and
	// refuse with ErrClassMismatch so the caller gets an explicit, catchable
	// error instead of corrupting the allocation. Tasks that are currently in
	// the root group (unallocated) are allowed to be placed into the group's
	// class, which is the normal attribution path.
	cur, err := m.controlGroupOfPID(pid)
	if err != nil {
		return fmt.Errorf("failed to determine current control group for pid %d (key %s): %w", pid, key, err)
	}
	if cur != "" && cur != e.rdtClass {
		return fmt.Errorf("%w: pid %d is in control group %q but mon_group %q is under %q",
			ErrClassMismatch, pid, cur, key, classDisplay(e.rdtClass))
	}

	tasksPath := filepath.Join(e.dir, tasksFile)
	f, err := os.OpenFile(tasksPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open tasks file for key %s: %w", key, err)
	}
	defer f.Close()

	data := []byte(strconv.Itoa(pid) + "\n")
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write pid %d for key %s: %w", pid, key, err)
	}
	log.Info("assigned PID to mon_group", "key", key, "pid", pid)
	return nil
}

// controlGroupOfPID returns the non-root resctrl ctrl_group name that
// currently owns pid, or "" if pid is not in any non-root control group (i.e.
// it is in the root group or not managed by resctrl).
//
// Only non-root ctrl_groups are scanned. Their tasks files list just the
// explicitly-allocated tasks (small), whereas the root tasks file enumerates
// every task on the system. Scanning only the class directories is sufficient
// for the AssignPID guard, which needs to detect whether pid already belongs
// to a *different* allocation class before it would be silently moved.
func (m *Manager) controlGroupOfPID(pid int) (string, error) {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return "", fmt.Errorf("failed to read resctrl root %s: %w", m.root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip kernel-managed directories; everything else at the root is a
		// user-created ctrl_group.
		if name == monGroupsDir || name == "mon_data" || name == "info" {
			continue
		}
		found, err := pidInTasksFile(filepath.Join(m.root, name, tasksFile), pid)
		if err != nil {
			return "", err
		}
		if found {
			return name, nil
		}
	}
	return "", nil
}

// pidInTasksFile reports whether pid appears in the resctrl tasks file at path.
// A missing tasks file (ENOENT) is treated as "not present" rather than an
// error, so an out-of-band removed ctrl_group does not fail the scan.
func pidInTasksFile(path string, pid int) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to open tasks file %s: %w", path, err)
	}
	defer f.Close()

	target := strconv.Itoa(pid)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == target {
			return true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return false, fmt.Errorf("failed to read tasks file %s: %w", path, err)
	}
	return false, nil
}

// classDisplay renders an rdtClass name for error messages, mapping the empty
// string (root ctrl_group) to a readable label.
func classDisplay(c string) string {
	if c == "" {
		return "root"
	}
	return c
}

// Remove deletes the mon_group for key (kernel releases the RMID) and drops
// all in-memory state for that key. Removing a directory that is already gone
// on disk is not an error, but calling Remove for a key that is not tracked
// returns ErrNotTracked.
//
// The lock is held across the on-disk removal so that a concurrent EnsureGroup
// cannot create a new entry that is immediately clobbered, and so that a failed
// rmdir leaves the entry intact (retryable) without risk of state corruption.
// Note: AssignPID/ReadCounters may still observe the entry (fetched before
// Remove acquires the lock) and attempt I/O on the now-removed directory.
// AssignPID will return an error wrapping ENOENT; ReadCounters will return
// empty readings (it treats a missing mon_data directory as a non-error).
// Both outcomes are benign transient races — the key will be gone from the map
// by the time the caller retries.
func (m *Manager) Remove(key string) error {
	key = m.canonKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entries[key]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotTracked, key)
	}

	if err := m.rmdir(e.dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Leave the entry in place so the caller can retry; state stays
		// consistent with on-disk reality.
		return fmt.Errorf("failed to remove mon_group %s: %w", e.dir, err)
	}

	delete(m.entries, key)
	log.Info("removed mon_group", "key", key, "dir", e.dir)
	return nil
}

// Reconcile removes on-disk mon_groups whose key is not present in live.
// Eligibility for removal requires all of:
//   - the directory name satisfies KeyValidator (scopes reaping to recognized
//     name shapes — e.g. UUID-shaped names with PodUIDValidator);
//   - the canonicalized name is NOT in the live set;
//   - the directory is not the authoritative path for a tracked entry.
//
// Kernel metadata directories (mon_groups, mon_data, info) are always skipped.
//
// Note: Reconcile identifies groups by their directory name (the key). If the
// same key were to appear under multiple ctrl_groups due to misconfiguration,
// only the in-memory tracked instance is authoritative; duplicates under other
// ctrl_groups are treated as orphans and removed.
func (m *Manager) Reconcile(live []string) error {
	liveSet := make(map[string]struct{}, len(live))
	for _, k := range live {
		liveSet[m.canonKey(k)] = struct{}{}
	}

	// Scan root-level mon_groups.
	m.reconcileDir(filepath.Join(m.root, monGroupsDir), liveSet)

	// Scan ctrl_group-level mon_groups.
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return fmt.Errorf("reconcile: failed to read resctrl root %s: %w", m.root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip kernel-managed directories at the resctrl root. Only
		// mon_groups, mon_data, and info are kernel-created; all other
		// directories are user-created ctrl_groups.
		if name == monGroupsDir || name == "mon_data" || name == "info" {
			continue
		}
		m.reconcileDir(filepath.Join(m.root, name, monGroupsDir), liveSet)
	}
	return nil
}

// reconcileDir scans a single mon_groups/ directory and removes orphan or
// stale-duplicate groups. A directory is reaped if:
//   - its canonicalized name is tracked by this Manager but at a different path
//     (stale duplicate after a class change — tasks exclusivity guarantees the
//     PIDs reside in the authoritative directory, so the duplicate is empty);
//   - its canonicalized name is neither tracked nor in liveSet (true orphan).
//
// Directories whose key appears in liveSet but is NOT tracked by this Manager
// are assumed to be owned by external tooling and are left untouched.
func (m *Manager) reconcileDir(monGroupsPath string, liveSet map[string]struct{}) {
	entries, err := os.ReadDir(monGroupsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn("reconcile: failed to read mon_groups dir", "path", monGroupsPath, "err", err)
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// Skip the kernel-managed directories that appear inside mon_groups/.
		// Only "info" appears here on current kernels; mon_data and
		// mon_groups are not nested under mon_groups/.
		if name == "info" || name == "mon_data" || name == "mon_groups" {
			continue
		}

		// Only consider directories whose name passes validation (e.g. the
		// pod-UID shape), which scopes reaping to groups this mechanism owns.
		if !m.validKey(name) {
			continue
		}

		// Canonicalize the on-disk name before lookup so it matches the
		// canonicalized keys in liveSet (e.g. case-folding, dash insertion).
		canon := m.canonKey(name)
		orphanDir := filepath.Join(monGroupsPath, name)

		// Three-way decision using tasks-exclusivity: a PID can only reside
		// in one mon_group at a time, so the Manager's tracked entry path is
		// definitively authoritative and any duplicate elsewhere is stale.
		m.mu.Lock()
		e, tracked := m.entries[canon]
		m.mu.Unlock()

		switch {
		case tracked && e.dir == orphanDir:
			// This IS the authoritative directory for a tracked key — keep.
			continue
		case tracked:
			// Stale duplicate: same key tracked under a different ctrl_group.
			// Tasks-exclusivity guarantees PIDs are in e.dir, so this directory
			// only wastes an RMID. Fall through to reap.
		case !tracked:
			if _, live := liveSet[canon]; live {
				// Key is live but not tracked by this Manager (e.g. managed
				// by an external OCI hook). Don't reap what we don't own.
				continue
			}
			// Not tracked, not in liveSet — true orphan. Fall through to reap.
		}

		// Reap the orphan/duplicate. Hold the lock across rmdir so a
		// concurrent EnsureGroup cannot have its freshly-created directory
		// reaped.
		m.mu.Lock()
		// Re-check: a concurrent EnsureGroup may have (re)claimed this
		// exact directory between our earlier check and acquiring the lock.
		if e, ok := m.entries[canon]; ok && e.dir == orphanDir {
			m.mu.Unlock()
			continue
		}
		if err := m.rmdir(orphanDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.mu.Unlock()
			log.Warn("reconcile: failed to remove orphan", "dir", orphanDir, "err", err)
			continue
		}
		m.mu.Unlock()
		log.Info("reconcile: removed orphan mon_group", "key", name, "dir", orphanDir)
	}
}

// List returns the keys currently tracked in memory.
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.entries))
	for k := range m.entries {
		keys = append(keys, k)
	}
	return keys
}

// Snapshot returns a point-in-time map of tracked key -> *Group handle. The
// returned Group values are copies safe to use after the lock is released.
func (m *Manager) Snapshot() map[string]*Group {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]*Group, len(m.entries))
	for k, e := range m.entries {
		out[k] = &Group{key: k, dir: e.dir, class: e.rdtClass}
	}
	return out
}
