# resctrl-mon: Per-Workload Memory Bandwidth & Cache Monitoring

`pkg/monitor` provides a runtime-agnostic library for managing resctrl
`mon_group` directories — one per workload — so that hardware monitoring
counters (MBM, CMT, Intel AET) can be attributed to individual workloads by
downstream tools.

## Background

The Linux [resctrl](https://www.kernel.org/doc/Documentation/x86/intel_rdt_ui.txt)
filesystem exposes hardware monitoring through *monitoring groups*
(`mon_group` directories). Each group is allocated a hardware monitoring ID
(RMID) by the kernel when the directory is created, and PIDs written to the
group's `tasks` file are accounted against that RMID. Counter values are read
back from the group's `mon_data/<domain>/*` files.

`pkg/monitor` manages the lifecycle of these per-workload groups: creating
them, assigning PIDs, reading counters with typed semantics, removing groups
(releasing the RMID), and reconciling orphaned groups left behind by crashes.
It deliberately knows nothing about any specific container runtime or
orchestrator.

## API

The API is described in
[pkg.go.dev](https://pkg.go.dev/github.com/intel/goresctrl/pkg/monitor).

## `pkg/monitor` vs `pkg/rdt`

Both packages can create and read resctrl monitoring groups, but they target
different models:

- Use **`pkg/rdt`** for config-driven RDT allocation (cache/memory-bandwidth
  partitioning via `CtrlGroup`s) with bundled L3 (CMT/MBM) monitoring and
  built-in Prometheus/OpenTelemetry export. Its monitoring API
  (`CreateMonGroup`, `GetMonData`, `NewCollector`) is coupled to the
  `Initialize` + `SetConfig` lifecycle and scoped to the L3 resource.
- Use **`pkg/monitor`** for standalone per-workload `mon_group` lifecycle
  management decoupled from allocation: no `Initialize`/`SetConfig`, placement
  under any (optionally `pkg/rdt`-managed) ctrl_group, typed counter readings
  (gauge vs cumulative, with units) across all `mon_data` domains — including
  Intel AET energy and PERF_PKG counters — key validation/canonicalization,
  pre-fork PID assignment, and crash recovery via `Reconcile`.

In short: `pkg/rdt` monitors the classes it allocates (L3, for export);
`pkg/monitor` manages per-workload group lifecycle and reads arbitrary
counters, independent of allocation.

## Consumers

`pkg/monitor` is a library. Integration adapters that map a runtime's
workload lifecycle onto `Manager` calls (for example an NRI plugin that keys
groups by pod UID) live in their own repositories and import only
`pkg/monitor`. The library exposes pluggable `KeyValidator` and
`KeyCanonicalizer` hooks (see below) so that an adapter can constrain and
normalize the keys it uses as directory names.

## Group Naming and Reconcile Scoping

Each mon_group directory is named by its bare key (e.g. a pod UID):

- `mon_groups/<key>`

This matches the convention used by downstream readers, which read the
directory name verbatim as the workload identifier — no prefix to strip.

`Manager.Reconcile()` is scoped by the configured `KeyValidator`: only
directories whose name satisfies the validator are eligible for orphan
removal. A consumer that configures `PodUIDValidator`, for example, causes
reconcile to consider only UUID-shaped directories — any directory whose name
does not match the UUID pattern is unconditionally skipped (kernel metadata,
directories from other tools with non-UUID names, etc.). Directories that DO
match the validator but are absent from the live set will be removed, so
callers must pass a complete set of currently-active keys.

`PodUIDValidator` accepts a pod UID in either the standard dashed
`8-4-4-4-12` form (reported by containerd) or the compact 32-character
hex form (reported by some CRI-O versions). Pairing it with
`CanonicalizePodUID` via `Options.KeyCanonicalizer` makes the manager insert
dashes for compact UIDs, so the on-disk `mon_groups/<uid>` directory name is
always the canonical dashed UUID regardless of which runtime created it.

## Library API

```go
import "github.com/intel/goresctrl/pkg/monitor"

// Validate: check resctrl is available and discover supported counters.
counters, err := monitor.Validate("/sys/fs/resctrl")
// counters: ["llc_occupancy", "mbm_total_bytes", "core_energy", ...]

mgr, _ := monitor.New(monitor.Options{
    ResctrlRoot:      "/sys/fs/resctrl",        // default
    KeyValidator:     monitor.PodUIDValidator,   // or nil for standalone
    KeyCanonicalizer: monitor.CanonicalizePodUID, // dash-normalize pod UIDs
})

// Create mon_group (idempotent)
grp, _ := mgr.EnsureGroup(podUID, rdtClass)

// Assign PID (pre-fork window for race-free attribution)
mgr.AssignPID(podUID, pid)

// On teardown (e.g. pod sandbox removal): delete the group, release the RMID.
// Tie the group's lifetime to the pod, not to individual containers — a
// container restart reuses the sandbox, and releasing the RMID early would
// hand the replacement a fresh RMID whose counters carry residual values.
mgr.Remove(podUID)

// Crash recovery: remove orphaned groups not in the live set
mgr.Reconcile(liveKeys)

// Read counters with typed semantics
readings, _ := mgr.ReadCounters(podUID)
for _, r := range readings {
    // r.Kind: monitor.Gauge (instantaneous) or monitor.Cumulative (monotonic counter)
    // r.Unit: UCUM where available ("By", "J"), otherwise descriptive ("farads", "")
    fmt.Printf("%s/%s = %f (%v, %s)\n", r.Domain, r.Name, r.Value, r.Kind, r.Unit)
}
```

## RMID Exhaustion

Each mon_group consumes one RMID from a pool with a size that is platform
dependent. When RMIDs are exhausted, `EnsureGroup` returns `monitor.ErrNoRMIDs`
(wrapping `ENOSPC`). Consumers can choose to treat this as non-fatal — e.g.
log a warning and let the workload start without monitoring — so that
monitoring degrades gracefully.

Check available RMIDs:
```bash
grep . /sys/fs/resctrl/info/*/num_rmids
find /sys/fs/resctrl -name tasks -path '*/mon_groups/*' | wc -l
```

## Requirements

- Linux kernel with resctrl monitoring support (CMT/MBM); Intel AET energy
  counters additionally require kernel ≥ 7.0 with `CONFIG_X86_CPU_RESCTRL=y`
- resctrl mounted at `/sys/fs/resctrl` (the default mount point)

Race-free RMID assignment requires the consumer to write a workload's init
PID to the group's `tasks` file before the workload forks its payload (see
`AssignPID`). Adapters built on container runtimes therefore need a runtime
that exposes the container PID at the appropriate point in the lifecycle.

## Related Packages

- `pkg/rdt`: Config-driven resctrl allocation (ctrl_groups, schemata).
  `pkg/monitor` creates mon_groups *under* ctrl_groups that may be managed by
  `pkg/rdt`, but does not depend on `pkg/rdt` or its config machinery.
