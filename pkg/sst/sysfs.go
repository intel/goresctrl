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
	"github.com/intel/goresctrl/pkg/utils"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type cpuPackageInfo struct {
	id   int
	cpus []int
}

func getOnlineCpuPackages() (map[int]*cpuPackageInfo, error) {
	basePath := "/sys/bus/cpu/devices"

	files, err := ioutil.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	pkgs := make(map[int]*cpuPackageInfo)

	for _, file := range files {
		// Try to read siblings from topology
		raw, err := ioutil.ReadFile(filepath.Join(basePath, file.Name(), "topology/physical_package_id"))
		if os.IsNotExist(err) {
			// Offline -> topology information does not exist
			continue
		} else if err != nil {
			return nil, err
		}

		cpuId, err := strconv.Atoi(file.Name()[3:])
		if err != nil {
			return nil, err
		}

		pkgId, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, err
		}

		if _, ok := pkgs[pkgId]; !ok {
			pkgs[pkgId] = &cpuPackageInfo{id: pkgId}
		}
		pkgs[pkgId].cpus = append(pkgs[pkgId].cpus, cpuId)
	}

	return pkgs, nil
}

func setCPUFreqValue(cpu utils.ID, setting string, value int) error {
	str := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/%s", cpu, setting)

	if err := ioutil.WriteFile(str, []byte(strconv.Itoa(value)), 0644); err != nil {
		return err
	}

	return nil
}

func getCPUFreqValue(cpu utils.ID, setting string) (int, error) {
	str := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/%s", cpu, setting)

	raw, err := ioutil.ReadFile(str)
	if err != nil {
		return 0, err
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, err
	}

	return value, nil
}

func setCPUScalingMin2CPUInfoMinFreq(cpu utils.ID) error {
	freq, err := getCPUFreqValue(cpu, "cpuinfo_min_freq")
	if err != nil {
		return err
	}

	return setCPUScalingMinFreq(cpu, freq)
}

func setCPUScalingMin2CPUInfoMaxFreq(cpu utils.ID) error {
	freq, err := getCPUFreqValue(cpu, "cpuinfo_max_freq")
	if err != nil {
		return err
	}

	return setCPUScalingMinFreq(cpu, freq)
}

func setCPUScalingMinFreq(cpu utils.ID, freq int) error {
	return setCPUFreqValue(cpu, "scaling_min_freq", freq)
}

func isHWPEnabled() bool {
	status, err := utils.ReadMSR(0, MSR_PM_ENABLE)
	if err != nil {
		return false
	}

	return (status & 0xff) != 0
}
