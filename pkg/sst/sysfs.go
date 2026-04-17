/*
Copyright 2021 Intel Corporation

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

type cpuPackageInfo struct {
	id   uint8
	cpus []uint16
}

func (pkg *cpuPackageInfo) hasCpus(cpus utils.IDSet) bool {
	pkgCPUs := make(utils.IDSet, len(pkg.cpus))
	for _, c := range pkg.cpus {
		pkgCPUs.Add(utils.ID(c))
	}
	return pkgCPUs.Has(cpus.Members()...)
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
			pkgs[int(pkgId)] = &cpuPackageInfo{id: uint8(pkgId)}
		}
		pkgs[int(pkgId)].cpus = append(pkgs[int(pkgId)].cpus, uint16(cpuId))
	}

	return pkgs, nil
}

func isHWPEnabled() (bool, error) {
	status, err := utils.ReadMSR(0, MSR_PM_ENABLE)
	if err != nil {
		return false, err
	}

	return (status & 0xff) != 0, nil
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

func setCPUScalingMin2CPUInfoMinFreq(cpu utils.ID) error {
	freq, err := utils.GetCPUFreqValue(cpu, "cpuinfo_min_freq")
	if err != nil {
		return err
	}

	return utils.SetCPUScalingMinFreq(cpu, freq)
}

func setCPUScalingMin2CPUInfoMaxFreq(cpu utils.ID) error {
	freq, err := utils.GetCPUFreqValue(cpu, "cpuinfo_max_freq")
	if err != nil {
		return err
	}

	return utils.SetCPUScalingMinFreq(cpu, freq)
}
