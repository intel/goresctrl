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
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AttributeFunc returns additional OTel attributes for a reading.
// Called for each group during collection. The key is the canonicalized
// tracking key (e.g. dashed pod UID), path is the absolute mon_group directory.
type AttributeFunc func(key, path string) []attribute.KeyValue

// FilterFunc returns whether a Reading should be exported. Return false to
// suppress the reading (e.g. to gate perf counters behind a config flag).
type FilterFunc func(Reading) bool

// OTelOption configures RegisterOTelInstruments.
type OTelOption func(*otelConfig)

type otelConfig struct {
	attrs  AttributeFunc
	filter FilterFunc
}

// WithAttributes sets a function that provides extra attributes per group.
// The attributes are appended to the default domain.id and domain.name
// attributes on each observed metric value.
func WithAttributes(fn AttributeFunc) OTelOption {
	return func(c *otelConfig) { c.attrs = fn }
}

// WithFilter sets a function that gates which readings are exported. If the
// filter returns false, the reading is silently skipped.
func WithFilter(fn FilterFunc) OTelOption {
	return func(c *otelConfig) { c.filter = fn }
}

// Registration is a placeholder return type for RegisterOTelInstruments.
// It currently carries no state; future versions may add deregistration support.
type Registration struct{}

// RegisterOTelInstruments registers observable OTel instruments for all
// counters readable by the Manager. A batch callback is registered with the
// provided Meter that calls Snapshot + ReadCounters on each collection cycle.
//
// Instrument names are derived mechanically from the resctrl domain and
// counter file names:
//
//	mon_L3_00/llc_occupancy          → l3.llc.occupancy           (unit: By)
//	mon_L3_00/mbm_local_bytes        → l3.mbm.local.bytes         (unit: By)
//	mon_L3_00/mbm_total_bytes        → l3.mbm.total.bytes         (unit: By)
//	mon_PERF_PKG_00/core_energy      → perf.core.energy            (unit: J)
//	mon_PERF_PKG_00/activity         → perf.activity                (unit: farads)
//	mon_PERF_PKG_00/c1_res           → perf.c1.res
//	mon_PERF_PKG_00/c6_res           → perf.c6.res
//	mon_PERF_PKG_00/uops_retired     → perf.uops.retired
//	mon_PERF_PKG_00/unhalted_core_cycles → perf.unhalted.core.cycles
//	mon_PERF_PKG_00/unhalted_ref_cycles  → perf.unhalted.ref.cycles
//	mon_PERF_PKG_00/stalls_llc_miss  → perf.stalls.llc.miss
//	mon_PERF_PKG_00/stalls_llc_hit   → perf.stalls.llc.hit
//
// The L3 counters and core_energy/activity are always present when the
// hardware supports them. The remaining PERF_PKG counters (c1_res, c6_res,
// uops_retired, unhalted_*_cycles, stalls_*) require the kernel command-line
// parameter "rdt=perf". Counter names are platform-dependent; the library
// registers instruments for whatever files the kernel exposes.
//
// The domain prefix is the first component after "mon_" (lowercased):
// "mon_L3_00" → "l3", "mon_PERF_PKG_00" → "perf". Multi-word qualifiers
// like PKG describe the instance scope; the full domain directory name is
// available via the "domain.name" attribute (e.g. "mon_PERF_PKG_00").
//
// L3 instrument names are similar to pkg/rdt's RegisterOpenTelemetryInstruments
// but preserve the _bytes counter suffix (e.g. l3.mbm.total.bytes vs
// pkg/rdt's l3.mbm.total). This maintains backward compatibility with the
// kernel counter file names as a mechanical derivation.
//
// Each metric carries a "domain.id" attribute with the numeric instance
// (e.g. "00") and a "domain.name" attribute with the full domain directory
// name (e.g. "mon_PERF_PKG_00"). Additional per-group attributes can be
// injected via WithAttributes.
//
// Monotonic counters (ReadingKind == Cumulative) are smoothed via an internal
// accumulator that suppresses brief negative deltas caused by cross-CPU
// aggregation races in the kernel.
//
// The caller owns the Meter and MeterProvider lifecycle (endpoint, push
// interval, shutdown).
func (m *Manager) RegisterOTelInstruments(meter metric.Meter, opts ...OTelOption) (*Registration, error) {
	cfg := &otelConfig{}
	for _, o := range opts {
		o(cfg)
	}

	obs := &otelObserver{
		mgr:    m,
		cfg:    cfg,
		meter:  meter,
		instrs: make(map[string]metric.Observable),
		accum:  newOTelAccumulator(),
	}

	// Discover instruments from the root mon_data directory. This gives us
	// the set of (domain-prefix, counter) pairs to pre-register.
	if err := obs.discoverAndRegister(); err != nil {
		return nil, err
	}

	return &Registration{}, nil
}

// otelObserver holds state for the OTel batch callback.
type otelObserver struct {
	mgr   *Manager
	cfg   *otelConfig
	meter metric.Meter

	mu     sync.Mutex
	instrs map[string]metric.Observable // instrName → instrument
	accum  *otelAccumulator
}

// discoverAndRegister scans root/mon_data to find available (domain, counter)
// pairs by walking directory entries and pre-registers instruments + a batch
// callback. Counter files are discovered by filename only — their contents are
// not read — so instruments are registered even when the kernel reports
// temporary placeholder values like "Unavailable".
func (o *otelObserver) discoverAndRegister() error {
	counters, err := discoverCounters(filepath.Join(o.mgr.root, "mon_data"))
	if err != nil {
		return err
	}

	// Deduplicate by instrument name (many domains share counter names but
	// only the prefix differs by domain type, not by instance).
	seen := make(map[string]struct{})
	var observables []metric.Observable
	for _, c := range counters {
		name := InstrumentName(c.domain, c.counter)
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		instr, err := o.createInstrument(name, metaKind(c.counter), metaUnit(c.counter))
		if err != nil {
			return err
		}
		o.instrs[name] = instr
		observables = append(observables, instr)
	}

	if len(observables) == 0 {
		return nil
	}

	_, err = o.meter.RegisterCallback(
		func(ctx context.Context, obs metric.Observer) error {
			o.observe(ctx, obs)
			return nil
		},
		observables...,
	)
	return err
}

// counterEntry is a (domain, counter) pair discovered by walking directory entries.
type counterEntry struct {
	domain  string
	counter string
}

// discoverCounters walks mon_data subdirectories and returns counter file names
// without reading their contents. This ensures instruments are registered even
// when the kernel reports non-numeric placeholder values.
func discoverCounters(monDataPath string) ([]counterEntry, error) {
	domains, err := os.ReadDir(monDataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []counterEntry
	for _, d := range domains {
		if !d.IsDir() {
			continue
		}
		domainPath := filepath.Join(monDataPath, d.Name())
		files, err := os.ReadDir(domainPath)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			out = append(out, counterEntry{domain: d.Name(), counter: f.Name()})
		}
	}
	return out, nil
}

func (o *otelObserver) createInstrument(name string, kind ReadingKind, unit string) (metric.Observable, error) {
	switch kind {
	case Cumulative:
		var opts []metric.Float64ObservableCounterOption
		if unit != "" {
			opts = append(opts, metric.WithUnit(unit))
		}
		return o.meter.Float64ObservableCounter(name, opts...)
	default:
		var opts []metric.Float64ObservableGaugeOption
		if unit != "" {
			opts = append(opts, metric.WithUnit(unit))
		}
		return o.meter.Float64ObservableGauge(name, opts...)
	}
}

// observe is the batch callback invoked by the OTel SDK on each collection.
func (o *otelObserver) observe(ctx context.Context, obs metric.Observer) {
	groups := o.mgr.Snapshot()
	for key, g := range groups {
		readings, err := o.mgr.ReadCounters(key)
		if err != nil {
			continue
		}

		var groupAttrs []attribute.KeyValue
		if o.cfg.attrs != nil {
			groupAttrs = o.cfg.attrs(key, g.Path())
		}

		for _, r := range readings {
			if o.cfg.filter != nil && !o.cfg.filter(r) {
				continue
			}

			instrName := InstrumentName(r.Domain, r.Name)
			o.mu.Lock()
			instr := o.instrs[instrName]
			o.mu.Unlock()
			if instr == nil {
				log.Warn("otel: unknown counter skipped (not discovered at registration)",
					"instrument", instrName, "domain", r.Domain, "counter", r.Name)
				continue
			}

			val := r.Value
			if r.Kind == Cumulative {
				val = o.accum.monotonic(key, r.Domain, r.Name, val)
			}

			attrs := make([]attribute.KeyValue, 0, len(groupAttrs)+2)
			attrs = append(attrs, attribute.String("domain.id", DomainInstance(r.Domain)))
			attrs = append(attrs, attribute.String("domain.name", r.Domain))
			attrs = append(attrs, groupAttrs...)

			switch i := instr.(type) {
			case metric.Float64Observable:
				obs.ObserveFloat64(i, val, metric.WithAttributes(attrs...))
			}
		}
	}

	// Prune accumulator state for groups no longer tracked by the Manager.
	// We prune by group key (not individual counters) so that filtered or
	// temporarily-unreadable counters retain their accumulated values as
	// long as the group is alive.
	liveKeys := make(map[string]struct{}, len(groups))
	for key := range groups {
		liveKeys[key] = struct{}{}
	}
	o.accum.pruneStaleGroups(liveKeys)
}

// --- Naming helpers (exported for use by callers building custom export) ---

// InstrumentName derives the OTel instrument name from a resctrl domain
// directory name and counter file name.
//
// The counter file name is converted to dot-separated segments and prepended
// with the domain's resource prefix. The _bytes suffix is preserved (not
// stripped) so that the OTel→Prometheus bridge's unit-suffix deduplication
// produces correct names without colliding with the counter _total suffix
// convention.
//
// NOTE: This intentionally diverges from pkg/rdt's RegisterOpenTelemetryInstruments
// which uses names like "l3.mbm.total" (stripping _bytes). That approach
// produces incorrect Prometheus names via the OTel bridge: the bridge treats
// the trailing "total" as a counter suffix, yielding "l3_mbm_bytes_total"
// instead of the expected "l3_mbm_total_bytes_total". By preserving _bytes in
// the OTel name, the bridge sees the unit is already present and only appends
// _total for counters, producing the correct final name.
//
// Examples:
//
//	InstrumentName("mon_L3_00", "llc_occupancy")     = "l3.llc.occupancy"
//	InstrumentName("mon_L3_00", "mbm_local_bytes")   = "l3.mbm.local.bytes"
//	InstrumentName("mon_L3_00", "mbm_total_bytes")   = "l3.mbm.total.bytes"
//	InstrumentName("mon_PERF_PKG_01", "core_energy") = "perf.core.energy"
//	InstrumentName("mon_PERF_PKG_01", "c1_res")      = "perf.c1.res"
func InstrumentName(domain, counter string) string {
	prefix := DomainPrefix(domain)
	name := strings.ReplaceAll(counter, "_", ".")
	return prefix + "." + name
}

// DomainPrefix extracts the resource prefix from a resctrl domain directory
// name by taking the first component after "mon_" (lowercased).
// E.g. "mon_L3_00" → "l3", "mon_PERF_PKG_01" → "perf".
//
// Multi-word qualifiers (e.g. PKG in PERF_PKG) describe the instance scope
// (package vs core) and are available via the domain.name attribute rather
// than the instrument name prefix.
func DomainPrefix(domain string) string {
	s := strings.TrimPrefix(domain, "mon_")
	// Take only the first underscore-delimited component.
	if i := strings.Index(s, "_"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// DomainInstance extracts the numeric instance from a resctrl domain directory
// name. E.g. "mon_L3_00" → "00", "mon_PERF_PKG_01" → "01".
func DomainInstance(domain string) string {
	if i := strings.LastIndex(domain, "_"); i >= 0 {
		s := domain[i+1:]
		if _, err := strconv.Atoi(s); err == nil {
			return s
		}
	}
	return ""
}

// --- Monotonic accumulator ---

// otelAccumulator reconstructs monotonic cumulative sums from raw hardware
// readings that may briefly decrease due to cross-CPU aggregation races.
type otelAccumulator struct {
	mu    sync.Mutex
	state map[string]*counterState
}

type counterState struct {
	prevRaw     float64
	accumulated float64
}

func newOTelAccumulator() *otelAccumulator {
	return &otelAccumulator{
		state: make(map[string]*counterState),
	}
}

// monotonic accumulates a non-negative delta; negative deltas (hardware race)
// are suppressed. Returns the monotonically-increasing accumulated value.
func (a *otelAccumulator) monotonic(group, domain, counter string, raw float64) float64 {
	k := group + "\x00" + domain + "\x00" + counter
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.state[k]
	if !ok {
		a.state[k] = &counterState{prevRaw: raw, accumulated: raw}
		return raw
	}
	if d := raw - s.prevRaw; d >= 0 {
		s.accumulated += d
	}
	// Negative delta: suppress (hardware aggregation race). Keep prevRaw
	// updated so the next positive delta is computed from the latest value.
	s.prevRaw = raw
	return s.accumulated
}

// pruneStaleGroups removes accumulator state for groups no longer in liveKeys.
// Pruning is by group key prefix so that filtered or temporarily-unreadable
// counters retain their accumulated values as long as the group is alive.
func (a *otelAccumulator) pruneStaleGroups(liveKeys map[string]struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.state {
		// Key format: group + "\x00" + domain + "\x00" + counter
		group := k[:strings.IndexByte(k, '\x00')]
		if _, ok := liveKeys[group]; !ok {
			delete(a.state, k)
		}
	}
}
