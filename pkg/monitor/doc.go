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

// Package monitor manages the lifecycle of per-workload resctrl monitoring
// groups (mon_groups). It provides runtime-agnostic operations for creating,
// assigning PIDs to, and removing mon_groups — independent of the config-driven
// pkg/rdt allocation model.
//
// The primary use case is assigning per-pod (or per-container) RMIDs so that
// downstream energy-monitoring tools like Kepler can attribute hardware energy
// counters (e.g. Intel AET) to individual workloads.
//
// # pkg/monitor vs pkg/rdt
//
// Both packages can create and read resctrl monitoring groups, but they target
// different models:
//
//   - Use pkg/rdt when you want config-driven RDT allocation (cache/memory
//     bandwidth partitioning via CtrlGroups) with bundled L3 (CMT/MBM)
//     monitoring and built-in Prometheus/OpenTelemetry export. Its monitoring
//     surface (CreateMonGroup, GetMonData, NewCollector) is coupled to the
//     Initialize + SetConfig lifecycle and is scoped to the L3 resource.
//
//   - Use pkg/monitor when you need standalone per-workload mon_group lifecycle
//     management decoupled from allocation: no Initialize/SetConfig required, group
//     placement under any (optionally pkg/rdt-managed) ctrl_group, typed counter
//     readings (gauge vs cumulative, with units) across all mon_data domains
//     including Intel AET energy and PERF_PKG counters, key validation and
//     canonicalization (e.g. pod UIDs), pre-fork PID assignment, and crash
//     recovery via Reconcile.
//
// In short: pkg/rdt monitors the classes it allocates (L3, for export);
// pkg/monitor manages per-workload group lifecycle and reads arbitrary
// counters, independent of allocation.
//
// Usage:
//
//	monitor.SetLogger(slog.Default().WithGroup("monitor"))
//
//	mgr, err := monitor.New(monitor.Options{
//	    KeyValidator:      monitor.PodUIDValidator,
//	    KeyCanonicalizer:  monitor.CanonicalizePodUID,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	grp, err := mgr.EnsureGroup(podUID, rdtClass)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := mgr.AssignPID(podUID, containerPID); err != nil {
//	    log.Fatal(err)
//	}
//
//	// On teardown:
//	mgr.Remove(podUID)
package monitor
