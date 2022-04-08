/*
Copyright 2022 Intel Corporation

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

package utils

import (
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
)

func setCPUFreqValue(cpu ID, setting string, value int) error {
	str := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/%s", cpu, setting)

	if err := ioutil.WriteFile(str, []byte(strconv.Itoa(value)), 0644); err != nil {
		return err
	}

	return nil
}

// GetCPUFreqValue returns information of the currently used CPU frequency
func GetCPUFreqValue(cpu ID, setting string) (int, error) {
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

// SetCPUScalingMinFreq sets the scaling_min_freq value of a given CPU
func SetCPUScalingMinFreq(cpu ID, freq int) error {
	return setCPUFreqValue(cpu, "scaling_min_freq", freq)
}

// SetCPUScalingMaxFreq sets the scaling_max_freq value of a given CPU
func SetCPUScalingMaxFreq(cpu ID, freq int) error {
	return setCPUFreqValue(cpu, "scaling_max_freq", freq)
}

// SetCPUsScalingMinFreq sets the scaling_min_freq value of a given set of CPUs
func SetCPUsScalingMinFreq(cpus []ID, freq int) error {
	for cpu := range cpus {
		if err := SetCPUScalingMinFreq(cpu, freq); err != nil {
			return err
		}
	}

	return nil
}

// SetCPUsScalingMaxFreq sets the scaling_max_freq value of a given set of CPUs
func SetCPUsScalingMaxFreq(cpus []ID, freq int) error {
	for cpu := range cpus {
		if err := SetCPUScalingMaxFreq(cpu, freq); err != nil {
			return err
		}
	}

	return nil
}
