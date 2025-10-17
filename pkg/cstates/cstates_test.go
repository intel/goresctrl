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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/intel/goresctrl/pkg/utils"
)

// mockFS implements sysfsIface for testing, allowing to override
// individual methods on-the-fly.
type mockFS struct {
	fPossibleCpus          func() (string, error)
	fCpuidleStates         func(cpu utils.ID) ([]int, error)
	fCpuidleStateAttrRead  func(cpu utils.ID, state int, attr string) (string, error)
	fCpuidleStateAttrWrite func(cpu utils.ID, state int, attr string, value string) error
}

func (fs *mockFS) PossibleCpus() (string, error) {
	return fs.fPossibleCpus()
}

func (fs *mockFS) CpuidleStates(cpu utils.ID) ([]int, error) {
	return fs.fCpuidleStates(cpu)
}

func (fs *mockFS) CpuidleStateAttrRead(cpu utils.ID, state int, attr string) (string, error) {
	return fs.fCpuidleStateAttrRead(cpu, state, attr)
}

func (fs *mockFS) CpuidleStateAttrWrite(cpu utils.ID, state int, attr string, value string) error {
	return fs.fCpuidleStateAttrWrite(cpu, state, attr, value)
}

func TestNewCstatesFromSysfs(t *testing.T) {
	// Make sure the platform supports cpuidle and disabling a C-state.
	if _, err := os.Stat("/sys/devices/system/cpu/cpu0/cpuidle/state1/disable"); os.IsNotExist(err) {
		t.Skip("/sys/devices/system/cpu/cpu0/cpuidle/state1/disable does not exist")
	}
	possibleCpus, err := NewSysfs().PossibleCpus()
	require.NoError(t, err, "Failed to get possible CPUs from sysfs")
	require.NotEqual(t, possibleCpus, "")

	cpus, err := utils.NewIDSetFromString(possibleCpus)
	require.NoError(t, err, "Failed to parse possible CPUs %q", possibleCpus)

	cpuCount := cpus.Size()

	// Read the disable attribute from all C-states of all CPUs.
	cs, err := NewCstatesFromSysfs(FilterAttrs(AttrDisable))
	require.NoError(t, err, "NewCstatesFromSysfs failed")

	// Compare read CPUs to the number of possible CPUs.
	require.Equal(t, cs.CPUs().Size(), cpuCount, "Expected %d CPUs, got %d", cpuCount, cs.CPUs().Size())
}

func TestCstatesWithMockFS(t *testing.T) {
	// This test runs in environments without access to cpuidle in
	// sysfs (like in virtual machines).
	cs := NewCstates()
	fs := &mockFS{
		fPossibleCpus: func() (string, error) {
			return "0-7", nil
		},
		fCpuidleStates: func(cpu utils.ID) ([]int, error) {
			return []int{0, 1, 2, 3, 4}, nil
		},
		fCpuidleStateAttrRead: func(cpu utils.ID, state int, attr string) (string, error) {
			// Fail if any other file will be read than filtered ones
			if cpu < 2 || cpu > 5 {
				t.Fatalf("Unexpected CPU ID %d", cpu)
			}
			if attr != "name" && (state != 1 && state != 2 && state != 4) {
				t.Fatalf("Unexpected read on cpu%d state%d attr %q", cpu, state, attr)
			}
			switch attr {
			case "name":
				return fmt.Sprintf("C%d", state), nil
			case "disable":
				if cpu == 3 && state > 0 {
					return "1", nil
				}
				return "0", nil
			case "time":
				return fmt.Sprintf("100%d%d", cpu, state), nil
			default:
				t.Fatalf("Unexpected read of attribute %q", attr)
			}
			return "", nil
		},
		fCpuidleStateAttrWrite: func(cpu utils.ID, state int, attr string, value string) error {
			t.Fatalf("Unexpected write of attribute %q", attr)
			return nil
		},
	}
	cs.fs = fs
	err := cs.Read(FilterCPUs(2, 3, 4, 5), FilterNames("C1", "C2", "C4"), FilterAttrs(AttrDisable, AttrTime))
	require.NoError(t, err, "Failed to populate C-states from sysfs")

	// Whatever happens next, nothing must be read from sysfs anymore.
	fs.fPossibleCpus = func() (string, error) {
		t.Fatalf("Unexpected read of possible CPUs while reads prohibited")
		return "", nil
	}
	fs.fCpuidleStates = func(cpu utils.ID) ([]int, error) {
		t.Fatalf("Unexpected read of cpuidle states while reads prohibited")
		return nil, nil
	}
	fs.fCpuidleStateAttrRead = func(cpu utils.ID, state int, attr string) (string, error) {
		t.Fatalf("Unexpected read on cpu%d state%d attr %q while reads prohibited", cpu, state, attr)
		return "", nil
	}

	// Compare read CPUs to the number of possible CPUs.
	require.Equal(t, cs.CPUs().SortedMembers(), []utils.ID{2, 3, 4, 5})

	require.Equal(t, cs.Names(), []string{"C1", "C2", "C4"})

	require.Equal(t, cs.Attrs(), []AttrID{AttrDisable, AttrTime})

	cpu2c1time := cs.GetAttr(2, "C1", AttrTime)
	require.NotNil(t, cpu2c1time, "CPU2 C1 time must not be nil")
	require.Equal(t, *cpu2c1time, "10021", "Unexpected CPU2 C1 time")

	cpu5c4time := cs.GetAttr(5, "C4", AttrTime)
	require.NotNil(t, cpu5c4time, "CPU5 C4 time must not be nil")
	require.Equal(t, *cpu5c4time, "10054")

	// Find which CPUs have C2, C3 (not even present) or C4 disabled.
	c2Disabled := cs.Copy(FilterNames("C2", "C3", "C4"), FilterAttrValues(AttrDisable, "1"))
	require.Equal(t, c2Disabled.CPUs().Members(), []utils.ID{3}, "CPU3, and only it, must have C2 disabled")

	// Enable C2 on CPU3.
	cpu3C2disable := "x"
	csFiltered := cs.Copy(FilterCPUs(3), FilterNames("C2"))
	// Override the fs write function to capture expected write, fail on all other writes.
	fs.fCpuidleStateAttrWrite = func(cpu utils.ID, state int, attr string, value string) error {
		if cpu == 3 && state == 2 && attr == "disable" {
			cpu3C2disable = value
			return nil
		}
		t.Fatalf("Unexpected write: cpu%d state%d attr: %q value: %q", cpu, state, attr, value)
		return nil
	}
	csFiltered.SetAttrs(AttrDisable, "0")
	require.NoError(t, csFiltered.Apply(), "Apply() failed")

	require.Equal(t, cpu3C2disable, "0", "Apply() was expected to write '0' to cpu3 state2 disable")

	csFiltered.SetAttrs(AttrDisable, "1")
	require.Equal(t, cpu3C2disable, "0", "Expected to keep '0' in cpu3 state2 disable until next Apply()")

	require.NoError(t, csFiltered.Apply(), "2nd Apply() failed")

	require.Equal(t, cpu3C2disable, "1", "2nd Apply() was expected to write '1' to cpu3 state2 disable")

	// Test that after clearing "disable" attributes Apply() will not write anything.
	// Force fail on every write.
	fs.fCpuidleStateAttrWrite = func(cpu utils.ID, state int, attr string, value string) error {
		t.Fatalf("Unexpected write: cpu%d state%d attr: %q value: %q", cpu, state, attr, value)
		return nil
	}
	csFiltered.ClearAttrs(AttrDisable)
	require.NoError(t, csFiltered.Apply(), "3rd Apply() failed")

	// Output the C-states with String().
	csStr := cs.String()
	t.Logf("cs=%s", csStr)
	require.Contains(t, csStr, "time=10021", "Expected to find time=10021 in C-states String()")

	csFilteredStr := csFiltered.String()
	t.Logf("csFiltered=%s", csFilteredStr)
	require.Contains(t, csFilteredStr, "time=10032", "Expected to find time=10032 in filtered C-states String()")
	require.NotContains(t, csFilteredStr, "time=10021", "Expected to not find time=10021 in filtered C-states String()")
	require.NotContains(t, csFilteredStr, "disable=", "Expected to not find cleared attribute disable in filtered C-states String()")
}
