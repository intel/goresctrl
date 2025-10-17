/*
Copyright 2025 Intel Corporation

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

package cstates

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/utils"
)

type sysfsIface interface {
	// PossibleCpus returns a string representing possible CPUs, e.g. "0-3".
	PossibleCpus() (string, error)
	// CpuidleStates returns a slice of cpuidle state numbers for the given CPU ID.
	// Numbers correspond to cpuidle/state<NUM> directories in sysfs.
	CpuidleStates(cpuID utils.ID) ([]int, error)
	// CpuidleStateAttrRead reads the value of the given attribute for the specified CPU ID and cpuidle state number.
	CpuidleStateAttrRead(cpu utils.ID, state int, attribute string) (string, error)
	// CpuidleStateAttrWrite writes the given value to the specified attribute for the given CPU ID and cpuidle state number.
	CpuidleStateAttrWrite(cpu utils.ID, state int, attribute string, value string) error
}

type sysfs struct{}

func NewSysfs() sysfsIface {
	return &sysfs{}
}

func (fs *sysfs) PossibleCpus() (string, error) {
	return fs.readString(goresctrlpath.Path("sys/devices/system/cpu/possible"))
}

func (fs *sysfs) CpuidleStates(cpu utils.ID) ([]int, error) {
	cpuidlePath := "sys/devices/system/cpu/cpu" + strconv.Itoa(cpu) + "/cpuidle"
	states := []int{}

	dirEntries, err := os.ReadDir(goresctrlpath.Path(cpuidlePath))
	if err != nil {
		return states, err
	}

	for _, entry := range dirEntries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "state") {
			state, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "state"))
			if err != nil {
				return states, fmt.Errorf("failed to parse cpuidle state dir name %q: %w", entry.Name(), err)
			}
			states = append(states, state)
		}
	}
	return states, nil
}

func (fs *sysfs) CpuidleStateAttrRead(cpu utils.ID, state int, attr string) (string, error) {
	return fs.readString(fs.cstateAttrPath(cpu, state, attr))
}

func (fs *sysfs) CpuidleStateAttrWrite(cpu utils.ID, state int, attr string, value string) error {
	return fs.writeString(fs.cstateAttrPath(cpu, state, attr), value)
}

func (fs *sysfs) cstateAttrPath(cpu utils.ID, state int, attr string) string {
	return "sys/devices/system/cpu/cpu" + strconv.Itoa(cpu) + "/cpuidle/state" + strconv.Itoa(state) + "/" + attr
}

func (fs *sysfs) readString(path string) (string, error) {
	data, err := os.ReadFile(goresctrlpath.Path(path))
	return strings.TrimSpace(string(data)), err
}

func (fs *sysfs) writeString(path string, data string) error {
	return os.WriteFile(goresctrlpath.Path(path), []byte(data), 0644)
}
