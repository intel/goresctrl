// Copyright 2026 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
)

// setupCPUs creates a fake cpufreq tree for the given CPUs under a temporary
// sysfs prefix, with every attribute initialized to the value "0".
func setupCPUs(t *testing.T, cpus ...ID) {
	t.Helper()
	dir := t.TempDir()
	goresctrlpath.SetPrefix(dir)
	t.Cleanup(func() { goresctrlpath.SetPrefix("") })
	for _, cpu := range cpus {
		cfDir := filepath.Join(dir, SysfsCpuBasepath, fmt.Sprintf("cpu%d", cpu), "cpufreq")
		if err := os.MkdirAll(cfDir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"scaling_governor", "scaling_min_freq", "scaling_max_freq"} {
			if err := os.WriteFile(filepath.Join(cfDir, name), []byte("0"), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func readCPUAttr(t *testing.T, cpu ID, name string) string {
	t.Helper()
	data, err := os.ReadFile(cpuFreqPath(cpu, name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func TestSetCPUScalingGovernor(t *testing.T) {
	setupCPUs(t, 0)
	if err := SetCPUScalingGovernor(0, "performance"); err != nil {
		t.Fatalf("SetCPUScalingGovernor: %v", err)
	}
	if got := readCPUAttr(t, 0, "scaling_governor"); got != "performance" {
		t.Errorf("governor = %q, want performance", got)
	}
}

func TestSetCPUScalingFreqs(t *testing.T) {
	setupCPUs(t, 3)
	if err := SetCPUScalingMinFreq(3, 800000); err != nil {
		t.Fatalf("SetCPUScalingMinFreq: %v", err)
	}
	if err := SetCPUScalingMaxFreq(3, 2400000); err != nil {
		t.Fatalf("SetCPUScalingMaxFreq: %v", err)
	}
	if got := readCPUAttr(t, 3, "scaling_min_freq"); got != "800000" {
		t.Errorf("scaling_min_freq = %q, want 800000", got)
	}
	if got := readCPUAttr(t, 3, "scaling_max_freq"); got != "2400000" {
		t.Errorf("scaling_max_freq = %q, want 2400000", got)
	}
}

func TestGetCPUFreqValue(t *testing.T) {
	setupCPUs(t, 1)
	if err := SetCPUScalingMaxFreq(1, 3000000); err != nil {
		t.Fatal(err)
	}
	if v, err := GetCPUFreqValue(1, "scaling_max_freq"); err != nil || v != 3000000 {
		t.Errorf("GetCPUFreqValue = %d, %v; want 3000000", v, err)
	}
	if _, err := GetCPUFreqValue(1, "no_such_setting"); err == nil {
		t.Error("GetCPUFreqValue of a missing setting should fail")
	}
	if err := SetCPUScalingGovernor(1, "powersave"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCPUFreqValue(1, "scaling_governor"); err == nil {
		t.Error("GetCPUFreqValue of a non-numeric setting should fail")
	}
}

func TestSetFreqsAndGovernorForCPUs(t *testing.T) {
	cpus := []ID{0, 1, 2}
	setupCPUs(t, cpus...)
	if err := SetScalingGovernorForCPUs(cpus, "schedutil"); err != nil {
		t.Fatalf("SetScalingGovernorForCPUs: %v", err)
	}
	if err := SetCPUsScalingMinFreq(cpus, 1000000); err != nil {
		t.Fatalf("SetCPUsScalingMinFreq: %v", err)
	}
	if err := SetCPUsScalingMaxFreq(cpus, 2000000); err != nil {
		t.Fatalf("SetCPUsScalingMaxFreq: %v", err)
	}
	for _, cpu := range cpus {
		if got := readCPUAttr(t, cpu, "scaling_governor"); got != "schedutil" {
			t.Errorf("cpu%d governor = %q, want schedutil", cpu, got)
		}
		if got := readCPUAttr(t, cpu, "scaling_min_freq"); got != "1000000" {
			t.Errorf("cpu%d min = %q, want 1000000", cpu, got)
		}
		if got := readCPUAttr(t, cpu, "scaling_max_freq"); got != "2000000" {
			t.Errorf("cpu%d max = %q, want 2000000", cpu, got)
		}
	}
}

func TestSetFreqsForCPUsStopsOnError(t *testing.T) {
	// CPU 7 has no cpufreq directory, so the write to it fails.
	setupCPUs(t, 0)
	if err := SetScalingGovernorForCPUs([]ID{0, 7}, "performance"); err == nil {
		t.Error("SetScalingGovernorForCPUs should fail for a missing CPU")
	}
	if err := SetCPUsScalingMinFreq([]ID{0, 7}, 1); err == nil {
		t.Error("SetCPUsScalingMinFreq should fail for a missing CPU")
	}
	if err := SetCPUsScalingMaxFreq([]ID{0, 7}, 1); err == nil {
		t.Error("SetCPUsScalingMaxFreq should fail for a missing CPU")
	}
}

func TestUncoreFreqAvailable(t *testing.T) {
	setupUncoreDie(t, nil)
	if !UncoreFreqAvailable() {
		t.Error("UncoreFreqAvailable = false with the uncore directory present")
	}
	goresctrlpath.SetPrefix(t.TempDir())
	if UncoreFreqAvailable() {
		t.Error("UncoreFreqAvailable = true without the uncore directory")
	}
}

func TestSetUncoreFreqClamping(t *testing.T) {
	setupUncoreDie(t, map[string]string{
		"initial_min_freq_khz": "800000",
		"initial_max_freq_khz": "2500000",
		"min_freq_khz":         "800000",
		"max_freq_khz":         "2500000",
	})

	tcases := []struct {
		name string
		set  func(pkg, die ID, freq int) error
		get  func(pkg, die ID) (int, error)
		freq int
		want int
	}{
		{"min in range", SetUncoreMinFreq, GetUncoreMinFreq, 1200000, 1200000},
		{"min below hw min", SetUncoreMinFreq, GetUncoreMinFreq, 100, 800000},
		{"min above hw max", SetUncoreMinFreq, GetUncoreMinFreq, 9000000, 2500000},
		{"max in range", SetUncoreMaxFreq, GetUncoreMaxFreq, 2000000, 2000000},
		{"max below hw min", SetUncoreMaxFreq, GetUncoreMaxFreq, 100, 800000},
		{"max above hw max", SetUncoreMaxFreq, GetUncoreMaxFreq, 9000000, 2500000},
	}
	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.set(0, 0, tc.freq); err != nil {
				t.Fatalf("set: %v", err)
			}
			if got, err := tc.get(0, 0); err != nil || got != tc.want {
				t.Errorf("got %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestSetUncoreFreqMissingDie(t *testing.T) {
	setupUncoreDie(t, nil)
	if err := SetUncoreMinFreq(0, 0, 1000000); err == nil {
		t.Error("SetUncoreMinFreq on a die without attributes should fail")
	}
	if err := SetUncoreMaxFreq(0, 0, 1000000); err == nil {
		t.Error("SetUncoreMaxFreq on a die without attributes should fail")
	}
}

func setupUncoreDie(t *testing.T, vals map[string]string) {
	t.Helper()
	dir := t.TempDir()
	goresctrlpath.SetPrefix(dir)
	t.Cleanup(func() { goresctrlpath.SetPrefix("") })
	dieDir := filepath.Join(dir, SysfsUncoreBasepath, "package_00_die_00")
	if err := os.MkdirAll(dieDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, val := range vals {
		if err := os.WriteFile(filepath.Join(dieDir, name), []byte(val), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUncoreFreqGettersAndReset(t *testing.T) {
	setupUncoreDie(t, map[string]string{
		"initial_min_freq_khz": "800000",
		"initial_max_freq_khz": "2500000",
		"min_freq_khz":         "2200000",
		"max_freq_khz":         "2300000",
	})

	if v, err := GetUncoreMinFreq(0, 0); err != nil || v != 2200000 {
		t.Errorf("GetUncoreMinFreq = %d, %v; want 2200000", v, err)
	}
	if v, err := GetUncoreMaxFreq(0, 0); err != nil || v != 2300000 {
		t.Errorf("GetUncoreMaxFreq = %d, %v; want 2300000", v, err)
	}
	if v, err := GetUncoreInitialMinFreq(0, 0); err != nil || v != 800000 {
		t.Errorf("GetUncoreInitialMinFreq = %d, %v; want 800000", v, err)
	}
	if v, err := GetUncoreInitialMaxFreq(0, 0); err != nil || v != 2500000 {
		t.Errorf("GetUncoreInitialMaxFreq = %d, %v; want 2500000", v, err)
	}

	if err := ResetUncoreFreq(0, 0); err != nil {
		t.Fatalf("ResetUncoreFreq: %v", err)
	}
	if v, _ := GetUncoreMinFreq(0, 0); v != 800000 {
		t.Errorf("min after reset = %d, want 800000", v)
	}
	if v, _ := GetUncoreMaxFreq(0, 0); v != 2500000 {
		t.Errorf("max after reset = %d, want 2500000", v)
	}
}

func TestUncoreFreqResetMissingDie(t *testing.T) {
	setupUncoreDie(t, nil)
	if err := ResetUncoreFreq(0, 0); err == nil {
		t.Error("ResetUncoreFreq on a die without attributes should fail")
	}
}
