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

package sst

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/utils"
)

// punitInfo holds topology information for one power domain (punit) within a package.
type punitInfo struct {
	id             uint8
	cpus           utils.IDSet
	cpuPunitCoreID map[utils.ID]utils.ID    // cpu to punit core ID
	coreCPUs       map[utils.ID]utils.IDSet // punit core ID to cpus
}

type cpuPackageInfo struct {
	id     uint8
	cpus   utils.IDSet
	punits []*punitInfo
}

func (pkg *cpuPackageInfo) hasCpus(cpus utils.IDSet) bool {
	return pkg.cpus.Has(cpus.Members()...)
}

func (pkg *cpuPackageInfo) punitForCPU(cpu uint16) *punitInfo {
	for _, pu := range pkg.punits {
		if pu.cpus.Has(utils.ID(cpu)) {
			return pu
		}
	}
	return nil
}

func getOnlineCpuPackages() (map[int]*cpuPackageInfo, error) {
	basePath := goresctrlpath.Path("sys/bus/cpu/devices")

	files, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	pkgs := make(map[int]*cpuPackageInfo)

	for _, file := range files {
		// Try to read siblings from topology
		raw, err := os.ReadFile(filepath.Join(basePath, file.Name(), "topology/physical_package_id"))
		if os.IsNotExist(err) {
			// Offline -> topology information does not exist
			continue
		} else if err != nil {
			return nil, err
		}

		cpuId, err := strconv.ParseUint(file.Name()[3:], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CPU id from %q: %w", file.Name(), err)
		}

		pkgId, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 8)
		if err != nil {
			return nil, fmt.Errorf("failed to parse package id: %w", err)
		}

		if _, ok := pkgs[int(pkgId)]; !ok {
			pkgs[int(pkgId)] = &cpuPackageInfo{id: uint8(pkgId), cpus: make(utils.IDSet)}
		}
		pkg := pkgs[int(pkgId)]
		pkg.cpus.Add(utils.ID(cpuId))
	}

	return pkgs, nil
}

// msrPMEnable is the Intel Hardware P-state (HWP) enable MSR.
const msrPMEnable int64 = 0x770

func isHWPEnabled() (bool, error) {
	status, err := utils.ReadMSR(0, msrPMEnable)
	if err != nil {
		return false, err
	}

	return (status & 0x01) != 0, nil
}

func getCPUSocketID(cpu uint16) (uint8, error) {
	raw, err := os.ReadFile(filepath.Join(goresctrlpath.Path("sys/bus/cpu/devices"), fmt.Sprintf("cpu%d", cpu), "topology/physical_package_id"))
	if err != nil {
		return 0, fmt.Errorf("failed to read socket id for cpu %d: %w", cpu, err)
	}
	id, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 8)
	if err != nil {
		return 0, fmt.Errorf("failed to parse socket id for cpu %d: %w", cpu, err)
	}
	return uint8(id), nil
}
