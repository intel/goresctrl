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

package sst

import (
	"fmt"
	"os"
	"slices"

	"github.com/intel/goresctrl/pkg/sst/internal/isst"
	"github.com/intel/goresctrl/pkg/utils"
)

// Platform is the main entry point for the SST API.
type Platform struct {
	backend
	packages map[int]*cpuPackageInfo
}

// SstSupported returns true if Intel Speed Select Technologies (SST) is
// supported by the system and can be interfaced via the Linux kernel device.
func SstSupported() bool {
	devPath := isst.DevPath()
	if _, err := os.Stat(devPath); err != nil {
		if !os.IsNotExist(err) {
			sstlog.Error("failed to access sst device", "path", devPath, "error", err)
		} else {
			sstlog.Debug("sst device does not exist", "path", devPath)
		}
		return false
	}
	return true
}

// Init initializes SST and returns a handle. The handle stores a snapshot of
// CPU topology of online CPUs at init time and is not updated on CPU hotplug.
func Init() (*Platform, error) {
	if !SstSupported() {
		return nil, fmt.Errorf("SST not supported on this system")
	}
	packages, err := getOnlineCpuPackages()
	if err != nil {
		return nil, fmt.Errorf("failed to read CPU topology: %w", err)
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("no CPU packages found")
	}
	h := &Platform{packages: packages}
	// Warm up API version detection
	_ = h.platformAPIVersion()
	// Resolve punit topology for all packages.
	for _, pkg := range packages {
		if err := h.resolvePunits(pkg); err != nil {
			return nil, fmt.Errorf("failed to resolve punit topology for package %d: %w", pkg.id, err)
		}
	}
	return h, nil
}

// Package returns a PackageHandle for the given package ID.
func (h *Platform) Package(pkgID int) (*Package, bool) {
	pkg, ok := h.packages[pkgID]
	if !ok {
		return nil, false
	}
	return &Package{h: h, pkg: pkg}, true
}

// Packages returns PackageHandles for all detected packages, sorted by package ID.
func (h *Platform) Packages() []*Package {
	ids := make([]int, 0, len(h.packages))
	for id := range h.packages {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := make([]*Package, 0, len(ids))
	for _, id := range ids {
		result = append(result, &Package{h: h, pkg: h.packages[id]})
	}
	return result
}

// GetCPUClosID returns the SST-CP CLOS id for a CPU.
func (h *Platform) GetCPUClosID(cpu utils.ID) (int, error) {
	return h.getCPUClosID(cpu)
}

// ClosCount returns the number of CLOS supported by the platform.
func (h *Platform) ClosCount() int {
	return h.closCount()
}

// ClosAssociate associates CPUs to a CLOS.
func (h *Platform) ClosAssociate(clos int, cpus utils.IDSet) error {
	if clos < 0 || clos >= numClos {
		return fmt.Errorf("invalid CLOS id %d (valid: 0-%d)", clos, numClos-1)
	}
	for _, cpu := range cpus.Members() {
		pkg := h.pkgForCPU(utils.ID(cpu))
		if pkg == nil {
			return fmt.Errorf("CPU %d not found in any package", cpu)
		}
		if err := h.closAssociate(pkg, utils.ID(cpu), clos); err != nil {
			return fmt.Errorf("failed to associate CPU %d to CLOS %d: %w", cpu, clos, err)
		}
	}
	return nil
}

// pkgForCPU returns the cpuPackageInfo for the package containing the given CPU.
func (h *Platform) pkgForCPU(cpu utils.ID) *cpuPackageInfo {
	for _, pkg := range h.packages {
		if pkg.cpus.Has(cpu) {
			return pkg
		}
	}
	return nil
}
