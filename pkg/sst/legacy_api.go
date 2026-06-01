/*
Copyright 2021-2026 Intel Corporation

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

// This file contains the legacy flat API, implemented as wrappers around the
// handle-based API.
package sst

import (
	"fmt"
	"math"

	"github.com/intel/goresctrl/pkg/utils"
)

// NumClos is the number of CLOS supported by SST-CP.
//
// Deprecated: Use Init and Platform.ClosCount instead.
const NumClos = 4

// SstPackageInfo contains status of Intel Speed Select Technologies (SST)
// for one CPU package, used by the legacy API.
//
// Deprecated: Use Init and Package.GetStatus instead.
type SstPackageInfo struct {
	// pkg is the CPU topology entry for this package.
	pkg *cpuPackageInfo

	// PP (Performance Profile) info
	PPSupported    bool
	PPLocked       bool
	PPVersion      int
	PPCurrentLevel int
	PPMaxLevel     int

	// Information about SST features at the currently active PP level
	CPSupported bool
	CPEnabled   bool
	CPPriority  CPPriorityType
	BFSupported bool
	BFEnabled   bool
	BFCores     utils.IDSet
	TFSupported bool
	TFEnabled   bool

	ClosInfo    [NumClos]SstClosInfo
	ClosCPUInfo ClosCPUSet
}

// SstClosInfo contains parameters of one CLOS of SST-CP.
//
// Deprecated: Use ClosConfig and ClosStatus instead.
type SstClosInfo struct {
	EPP                  int
	ProportionalPriority int
	MinFreq              int
	MaxFreq              int
	DesiredFreq          int
}

// ClosCPUSet contains a mapping from CLOS id to a set of CPU ids.
//
// Deprecated: CPU associations are now in ClosStatus.CPUs.
type ClosCPUSet map[int]utils.IDSet

// legacyBackend is a singleton backend used by the legacy API.
var legacyBackend backend

// CheckPackageCpus checks that a list of CPUs belong to a given package.
//
// Deprecated.
func CheckPackageCpus(info *SstPackageInfo, cpus utils.IDSet) bool {
	if info == nil {
		return false
	}
	return info.pkg.hasCpus(cpus)
}

// GetPackageInfo returns SST information for the given packages, or all packages
// if none are specified. Topology is read fresh on each call.
//
// Deprecated: Use Init and Package.GetStatus instead.
func GetPackageInfo(pkgs ...int) (map[int]*SstPackageInfo, error) {
	packages, err := getOnlineCpuPackages()
	if err != nil {
		return nil, fmt.Errorf("failed to determine cpu topology: %w", err)
	}

	var pkglist []int
	if len(pkgs) == 0 {
		for i := range packages {
			pkglist = append(pkglist, i)
		}
	} else {
		for _, i := range pkgs {
			if _, ok := packages[i]; !ok {
				return nil, fmt.Errorf("cpu package %d not present", i)
			}
			pkglist = append(pkglist, i)
		}
	}

	if err := populatePunits(&legacyBackend, packages); err != nil {
		return nil, err
	}

	result := make(map[int]*SstPackageInfo, len(pkglist))
	for _, i := range pkglist {
		pi, err := legacyBackend.getPackageStatus(packages[i])
		if err != nil {
			return nil, err
		}
		info, err := statusFromPackage(packages[i], pi)
		if err != nil {
			return nil, fmt.Errorf("failed to convert SST info for package %d: %w", i, err)
		}
		result[i] = info
	}

	return result, nil
}

// GetCPUClosID returns the SST-CP CLOS id that a CPU is associated with.
//
// Deprecated: Use Init and Platform.GetCPUClosID instead.
func GetCPUClosID(cpu utils.ID) (int, error) {
	return legacyBackend.getCPUClosID(cpu)
}

func assignCPU2Clos(info *SstPackageInfo, clos int) error {
	sstlog.Debug("assigning CPUs to SST CLOS", "closID", clos, "cpuset", info.ClosCPUInfo[clos].Members())

	for _, cpu := range info.ClosCPUInfo[clos].Members() {
		if err := legacyBackend.closAssociate(info.pkg, utils.ID(cpu), clos); err != nil {
			return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
		}
	}

	return nil
}

func setCPUScalingMin2CPUInfoMinFreq(cpus utils.IDSet) error {
	for _, cpu := range cpus.Members() {
		freq, err := utils.GetCPUFreqValue(cpu, "cpuinfo_min_freq")
		if err != nil {
			return err
		}
		if err := utils.SetCPUScalingMinFreq(cpu, freq); err != nil {
			return err
		}
	}
	return nil
}

func setCPUScalingMin2CPUInfoMaxFreq(cpus utils.IDSet) error {
	for _, cpu := range cpus.Members() {
		freq, err := utils.GetCPUFreqValue(cpu, "cpuinfo_max_freq")
		if err != nil {
			return err
		}
		if err := utils.SetCPUScalingMinFreq(cpu, freq); err != nil {
			return err
		}
	}
	return nil
}

func enableBF(info *SstPackageInfo) error {
	if !info.BFSupported {
		return fmt.Errorf("SST BF not supported")
	}
	for _, pu := range info.pkg.punits {
		if err := legacyBackend.bfSetStatus(info.pkg, pu, true); err != nil {
			return err
		}
	}
	return setCPUScalingMin2CPUInfoMaxFreq(info.pkg.cpus)
}

// EnableBF enables SST-BF and sets up CPU frequency scaling accordingly.
//
// Deprecated: Use Init and Package.BFEnable instead.
func EnableBF(pkgs ...int) error {
	if ok, err := isHWPEnabled(); err != nil {
		return fmt.Errorf("failed to determine if HWP is enabled")
	} else if !ok {
		return fmt.Errorf("HWP is not enabled")
	}

	infomap, err := GetPackageInfo(pkgs...)
	if err != nil {
		return err
	}

	for _, i := range infomap {
		if err := enableBF(i); err != nil {
			return err
		}
	}

	return nil
}

func disableBF(info *SstPackageInfo) error {
	if !info.BFSupported {
		return fmt.Errorf("SST BF not supported")
	}
	for _, pu := range info.pkg.punits {
		if err := legacyBackend.bfSetStatus(info.pkg, pu, false); err != nil {
			return err
		}
	}
	return setCPUScalingMin2CPUInfoMinFreq(info.pkg.cpus)
}

// DisableBF disables SST-BF and restores CPU frequency scaling minimums.
//
// Deprecated: Use Init and Package.BFDisable instead.
func DisableBF(pkgs ...int) error {
	infomap, err := GetPackageInfo(pkgs...)
	if err != nil {
		return err
	}

	for _, i := range infomap {
		if err := disableBF(i); err != nil {
			return err
		}
	}

	return nil
}

// ConfigureCP configures CPU-to-CLOS mappings and sets the CP priority type.
//
// Deprecated: Use Init, Platform.ClosAssociate, and Package.CPSetPriorityType instead.
func ConfigureCP(info *SstPackageInfo, priority int, cpu2clos *ClosCPUSet) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}

	if priority < 0 || priority > 1 {
		return fmt.Errorf("invalid CP priority value %d (valid 0 or 1)", priority)
	}

	if info.ClosCPUInfo == nil {
		info.ClosCPUInfo = make(map[int]utils.IDSet, len(*cpu2clos))
	}

	for clos, cpus := range *cpu2clos {
		info.ClosCPUInfo[clos] = cpus.Clone()

		// Remove the CPU from other CLOSes if found.
		for i := 0; i < NumClos; i++ {
			if i == clos {
				continue
			}
			for id := range cpus {
				info.ClosCPUInfo[i].Del(id)
			}
		}

		if err := assignCPU2Clos(info, clos); err != nil {
			return err
		}
	}

	info.CPPriority = CPPriorityType(priority)

	return nil
}

// ClosSetup stores the user-supplied CLOS configuration and writes it to hardware.
//
// Deprecated: Use Init and Package.ClosConfigure instead.
func ClosSetup(info *SstPackageInfo, clos int, closInfo *SstClosInfo) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}

	if clos < 0 || clos >= NumClos {
		return fmt.Errorf("invalid Clos value (%d)", clos)
	}

	if closInfo.MinFreq < 0 || closInfo.MinFreq > 255 {
		return fmt.Errorf("invalid min freq (%d)", closInfo.MinFreq)
	}

	if closInfo.MaxFreq < 0 || closInfo.MaxFreq > 255 {
		return fmt.Errorf("invalid max freq (%d)", closInfo.MaxFreq)
	}

	if closInfo.MinFreq > closInfo.MaxFreq {
		return fmt.Errorf("min freq %d must be smaller than max freq %d", closInfo.MinFreq, closInfo.MaxFreq)
	}

	if closInfo.DesiredFreq < 0 || closInfo.DesiredFreq > 255 {
		return fmt.Errorf("invalid value %d for desired freq", closInfo.DesiredFreq)
	}

	if closInfo.EPP < 0 || closInfo.EPP > 15 {
		return fmt.Errorf("invalid value %d for EPP", closInfo.EPP)
	}

	if closInfo.ProportionalPriority < 0 || closInfo.ProportionalPriority > 15 {
		return fmt.Errorf("invalid value %d for proportionalPriority", closInfo.ProportionalPriority)
	}

	info.ClosInfo[clos] = *closInfo

	closConfig := ClosConfig{
		ProportionalPriority: closInfo.ProportionalPriority,
		MinFreq:              closInfo.MinFreq * 100,
		MaxFreq:              closInfo.MaxFreq * 100,
		epp:                  closInfo.EPP,
		desiredFreq:          closInfo.DesiredFreq * 100,
	}

	for _, pu := range info.pkg.punits {
		if err := legacyBackend.closConfigure(info.pkg, pu, clos, &closConfig); err != nil {
			return err
		}
	}
	return nil
}

// ResetCPConfig brings the system to a known state: all CLOSes reset to defaults
// and all CPUs assigned to CLOS 0.
//
// Deprecated: Use Init and Package.CPReset instead.
func ResetCPConfig() error {
	packages, err := getOnlineCpuPackages()
	if err != nil {
		return fmt.Errorf("failed to determine cpu topology: %w", err)
	}

	if err := populatePunits(&legacyBackend, packages); err != nil {
		return err
	}

	for _, pkg := range packages {
		for _, pu := range pkg.punits {
			if err := legacyBackend.closResetConfiguration(pkg, pu); err != nil {
				return err
			}
		}
		for _, cpu := range pkg.cpus.Members() {
			if err := legacyBackend.closAssociate(pkg, cpu, 0); err != nil {
				return fmt.Errorf("failed to associate cpu %d to clos %d: %w", cpu, 0, err)
			}
		}
	}

	return nil
}

// EnableCP enables SST-CP for a package. The priority type from info is used.
// info.ClosCPUInfo must be populated (e.g. via ConfigureCP) before calling.
//
// Deprecated: Use Init and Package.CPEnable instead.
func EnableCP(info *SstPackageInfo) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}
	if !info.CPSupported {
		return fmt.Errorf("SST CP not supported")
	}

	if len(info.ClosCPUInfo) == 0 {
		return fmt.Errorf("failed to enable CP: Clos to CPU mapping missing")
	}

	for _, pu := range info.pkg.punits {
		if err := legacyBackend.cpSetStatus(info.pkg, pu, true); err != nil {
			return fmt.Errorf("failed to enable SST-CP: %v", err)
		}
	}
	info.CPEnabled = true

	return nil
}

// DisableCP disables SST-CP for a package.
// SST-TF must be disabled before calling this function.
//
// Deprecated: Use Init and Package.CPDisable instead.
func DisableCP(info *SstPackageInfo) error {
	if !info.CPSupported {
		return fmt.Errorf("SST CP not supported")
	}

	if info.TFEnabled {
		return fmt.Errorf("SST TF still enabled, disable it first")
	}

	for _, pu := range info.pkg.punits {
		if err := legacyBackend.cpSetStatus(info.pkg, pu, false); err != nil {
			return fmt.Errorf("failed to disable SST-CP: %v", err)
		}
	}
	info.CPEnabled = false

	return nil
}

// statusFromPackage converts a hierarchical PackageStatus to a flat SstPackageInfo.
// Feature state is taken from the lowest id punit.
func statusFromPackage(pkg *cpuPackageInfo, pi *PackageStatus) (*SstPackageInfo, error) {
	if len(pi.Punits) == 0 {
		return nil, fmt.Errorf("package %d has no punits in SST info", pkg.id)
	}

	// Find the primary punit (lowest ID) for feature state.
	minPunit := math.MaxInt
	for id := range pi.Punits {
		if id < minPunit {
			minPunit = id
		}
	}
	primary := pi.Punits[minPunit]

	closInfos := [NumClos]SstClosInfo{}
	for i, ci := range primary.Clos {
		if i >= NumClos {
			break
		}
		closInfos[i] = SstClosInfo{
			EPP:                  ci.Config.epp,
			ProportionalPriority: ci.Config.ProportionalPriority,
			MinFreq:              ci.Config.MinFreq / 100,
			MaxFreq:              ci.Config.MaxFreq / 100,
			DesiredFreq:          ci.Config.desiredFreq / 100,
		}
	}

	info := &SstPackageInfo{
		pkg:            pkg,
		PPSupported:    primary.PP.Supported,
		PPLocked:       primary.PP.Locked,
		PPVersion:      primary.PP.Version,
		PPCurrentLevel: primary.PP.CurrentLevel,
		PPMaxLevel:     primary.PP.MaxLevel,
		BFSupported:    primary.BF.Supported,
		BFEnabled:      primary.BF.Enabled,
		TFSupported:    primary.TF.Supported,
		TFEnabled:      primary.TF.Enabled,
		CPSupported:    primary.CP.Supported,
		CPEnabled:      primary.CP.Enabled,
		CPPriority:     primary.CP.Priority,
		ClosInfo:       closInfos,
		ClosCPUInfo:    make(ClosCPUSet),
	}
	if primary.BF.Cores != nil {
		info.BFCores = primary.BF.Cores.Clone()
	}

	// Merge per-CLOS CPU sets from all punits (package-wide association).
	for _, punit := range pi.Punits {
		for i, ci := range punit.Clos {
			if ci.CPUs == nil {
				continue
			}
			if info.ClosCPUInfo[i] == nil {
				info.ClosCPUInfo[i] = ci.CPUs.Clone()
			} else {
				for _, id := range ci.CPUs.Members() {
					info.ClosCPUInfo[i].Add(id)
				}
			}
		}
	}

	return info, nil
}

// populatePunits resolves punit topology for any package that hasn't had it populated yet.
func populatePunits(h *backend, packages map[int]*cpuPackageInfo) error {
	for _, pkg := range packages {
		if pkg.punits != nil {
			continue
		}
		if err := h.resolvePunits(pkg); err != nil {
			return fmt.Errorf("failed to resolve punit topology for package %d: %w", pkg.id, err)
		}
	}
	return nil
}
