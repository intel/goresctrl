// Copyright 2019-2021 Intel Corporation. All Rights Reserved.
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

package blockio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intel/goresctrl/pkg/testutils"
)

var knownIOSchedulers map[string]bool = map[string]bool{
	"bfq":         true,
	"cfq":         true,
	"deadline":    true,
	"kyber":       true,
	"mq-deadline": true,
	"none":        true,
	"noop":        true,
}

// TestSetConfig: unit tests for SetConfigFromFile(), SetConfigFromData(), and SetConfig().
func TestSetConfig(t *testing.T) {
	initialConf := map[string]BlockIOParameters{
		"classname": BlockIOParameters{},
	}
	emptyConf := map[string]BlockIOParameters{}
	goodConf := map[string]BlockIOParameters{
		"goodclass": NewBlockIOParameters(),
	}
	classBlockIO = copyConf(initialConf)

	err := SetConfigFromFile("/blockio-test/non-existent-file", true)
	testutils.VerifyError(t, err, 1, []string{"/blockio-test/non-existent-file", "failed to read"})
	testutils.VerifyDeepEqual(t, "effective configuration 1", initialConf, classBlockIO)

	badConfFile := testutils.CreateTempFile(t, "bad config contents.\n")
	emptyConfFile := testutils.CreateTempFile(t, "")
	goodConfFile := testutils.CreateTempFile(t, "Classes:\n  goodclass:\n")
	defer os.Remove(badConfFile)
	defer os.Remove(emptyConfFile)
	defer os.Remove(goodConfFile)

	for syntaxerror := 0; syntaxerror < 4; syntaxerror++ {
		classBlockIO, err = copyConf(initialConf), nil
		switch syntaxerror {
		case 0:
			err = SetConfigFromFile(badConfFile, false)
		case 1:
			err = SetConfigFromFile(badConfFile, true)
		case 2:
			err = SetConfigFromData([]byte("bad config."), false)
		case 3:
			err = SetConfigFromData([]byte("bad config."), true)
		}
		if syntaxerror < 2 {
			testutils.VerifyError(t, err, 1, []string{badConfFile})
		}
		testutils.VerifyError(t, err, 1, []string{"error unmarshaling"})
		testutils.VerifyDeepEqual(t,
			fmt.Sprintf("syntax error configuration %d", syntaxerror),
			initialConf, classBlockIO)
	}

	// Test valid ways to clear (reset) all classes
	for clear := 0; clear < 8; clear++ {
		classBlockIO, err = copyConf(initialConf), nil
		switch clear {
		case 0:
			err = SetConfigFromFile(emptyConfFile, false)
		case 1:
			err = SetConfigFromFile(emptyConfFile, true)
		case 2:
			err = SetConfigFromData([]byte(""), false)
		case 3:
			err = SetConfigFromData([]byte(""), true)
		case 4:
			err = SetConfig(nil, false)
		case 5:
			err = SetConfig(nil, true)
		case 6:
			err = SetConfig(&Config{}, false)
		case 7:
			err = SetConfig(&Config{}, true)
		}
		testutils.VerifyNoError(t, err)
		testutils.VerifyDeepEqual(t,
			fmt.Sprintf("clear conf %d", clear),
			emptyConf, classBlockIO)
	}

	err = SetConfigFromFile(goodConfFile, true)
	testutils.VerifyNoError(t, err)
	testutils.VerifyDeepEqual(t, "ok conf", goodConf, classBlockIO)
}

// copyConf returns a shallow copy of blockio class configuration.
func copyConf(orig map[string]BlockIOParameters) map[string]BlockIOParameters {
	result := map[string]BlockIOParameters{}
	for key, value := range orig {
		result[key] = value
	}
	return result
}

func TestClassNames(t *testing.T) {
	classBlockIO = map[string]BlockIOParameters{
		"a": BlockIOParameters{},
		"z": BlockIOParameters{},
		"b": BlockIOParameters{},
		"x": BlockIOParameters{},
		"c": BlockIOParameters{},
		"d": BlockIOParameters{},
	}
	classes := GetClasses()
	testutils.VerifyStringSlices(t,
		[]string{"a", "b", "c", "d", "x", "z"},
		classes)
	classBlockIO = map[string]BlockIOParameters{}
	classes = GetClasses()
	testutils.VerifyStringSlices(t,
		[]string{},
		classes)
}

// TestGetCurrentIOSchedulers: unit test for getCurrentIOSchedulers().
func TestGetCurrentIOSchedulers(t *testing.T) {
	currentIOSchedulers, err := getCurrentIOSchedulers()
	testutils.VerifyError(t, err, 0, nil)
	for blockDev, ioScheduler := range currentIOSchedulers {
		s, ok := knownIOSchedulers[ioScheduler]
		if !ok || !s {
			t.Errorf("unknown io scheduler %#v on block device %#v", ioScheduler, blockDev)
		}
	}
}

// TestConfigurableBlockDevices: unit tests for configurableBlockDevices().
func TestConfigurableBlockDevices(t *testing.T) {
	sysfsBlockDevs, err := filepath.Glob("/sys/block/*")
	if err != nil {
		sysfsBlockDevs = []string{}
	}
	devBlockDevs := []string{}
	for _, sysfsBlockDev := range sysfsBlockDevs {
		if strings.HasPrefix(sysfsBlockDev, "/sys/block/sd") || strings.HasPrefix(sysfsBlockDev, "/sys/block/vd") {
			devBlockDevs = append(devBlockDevs, strings.Replace(sysfsBlockDev, "/sys/block/", "/dev/", 1))
		}
	}
	t.Logf("test real block devices: %v", devBlockDevs)
	tcases := []struct {
		name                    string
		devWildcards            []string
		expectedErrorCount      int
		expectedErrorSubstrings []string
		expectedMatches         int
		disabled                bool
		disabledReason          string
	}{
		{
			name:               "no device wildcards",
			devWildcards:       nil,
			expectedErrorCount: 0,
		},
		{
			name:                    "bad wildcard",
			devWildcards:            []string{"/[-/verybadwildcard]"},
			expectedErrorCount:      1,
			expectedErrorSubstrings: []string{"verybadwildcard", "syntax error"},
		},
		{
			name:                    "not matching wildcard",
			devWildcards:            []string{"/dev/path that should not exist/*"},
			expectedErrorCount:      1,
			expectedErrorSubstrings: []string{"does not match any"},
		},
		{
			name:                    "two wildcards: empty string and a character device",
			devWildcards:            []string{"/dev/null", ""},
			expectedErrorCount:      2,
			expectedErrorSubstrings: []string{"\"/dev/null\" is a character device", "\"\" does not match any"},
		},
		{
			name:                    "not a device or even a file",
			devWildcards:            []string{"/proc", "/proc/meminfo", "/proc/notexistingfile"},
			expectedErrorCount:      3,
			expectedErrorSubstrings: []string{"\"/proc\" is not a device", "\"/proc/meminfo\" is not a device"},
		},
		{
			name:            "real block devices",
			devWildcards:    devBlockDevs,
			expectedMatches: len(devBlockDevs),
		},
	}
	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.disabled {
				t.Skip(tc.disabledReason)
			}
			realPlatform := defaultPlatform{}
			bdis, err := realPlatform.configurableBlockDevices(tc.devWildcards)
			testutils.VerifyError(t, err, tc.expectedErrorCount, tc.expectedErrorSubstrings)
			if len(bdis) != tc.expectedMatches {
				t.Errorf("expected %d matching block devices, got %d", tc.expectedMatches, len(bdis))
			}
		})
	}
}

// TestDevicesParametersToCgBlockIO: unit tests for devicesParametersToCgBlockIO().
func TestDevicesParametersToCgBlockIO(t *testing.T) {
	// switch real devicesParametersToCgBlockIO to call mockPlatform.configurableBlockDevices
	currentPlatform = mockPlatform{}
	tcases := []struct {
		name                    string
		dps                     []DevicesParameters
		iosched                 map[string]string
		expectedOci             *BlockIOParameters
		expectedErrorCount      int
		expectedErrorSubstrings []string
	}{
		{
			name: "all OCI fields",
			dps: []DevicesParameters{
				{
					Weight: "144",
				},
				{
					Devices:           []string{"/dev/sda"},
					ThrottleReadBps:   "1G",
					ThrottleWriteBps:  "2M",
					ThrottleReadIOPS:  "3k",
					ThrottleWriteIOPS: "4",
					Weight:            "50",
				},
			},
			iosched: map[string]string{"/dev/sda": "bfq"},
			expectedOci: &BlockIOParameters{
				Weight: 144,
				WeightDevice: DeviceWeights{
					{Major: 11, Minor: 12, Weight: 50},
				},
				ThrottleReadBpsDevice: DeviceRates{
					{Major: 11, Minor: 12, Rate: 1000000000},
				},
				ThrottleWriteBpsDevice: DeviceRates{
					{Major: 11, Minor: 12, Rate: 2000000},
				},
				ThrottleReadIOPSDevice: DeviceRates{
					{Major: 11, Minor: 12, Rate: 3000},
				},
				ThrottleWriteIOPSDevice: DeviceRates{
					{Major: 11, Minor: 12, Rate: 4},
				},
			},
		},
		{
			name: "later match overrides value",
			dps: []DevicesParameters{
				{
					Devices:         []string{"/dev/sda", "/dev/sdb", "/dev/sdc"},
					ThrottleReadBps: "100",
					Weight:          "110",
				},
				{
					Devices:         []string{"/dev/sdb", "/dev/sdc"},
					ThrottleReadBps: "300",
					Weight:          "330",
				},
				{
					Devices:         []string{"/dev/sdb"},
					ThrottleReadBps: "200",
					Weight:          "220",
				},
			},
			iosched: map[string]string{"/dev/sda": "bfq", "/dev/sdb": "bfq", "/dev/sdc": "cfq"},
			expectedOci: &BlockIOParameters{
				Weight: -1,
				WeightDevice: DeviceWeights{
					{Major: 11, Minor: 12, Weight: 110},
					{Major: 21, Minor: 22, Weight: 220},
					{Major: 31, Minor: 32, Weight: 330},
				},
				ThrottleReadBpsDevice: DeviceRates{
					{Major: 11, Minor: 12, Rate: 100},
					{Major: 21, Minor: 22, Rate: 200},
					{Major: 31, Minor: 32, Rate: 300},
				},
			},
		},
		{
			name: "invalid weights, many errors in different parameter sets",
			dps: []DevicesParameters{
				{
					Weight: "99999",
				},
				{
					Devices: []string{"/dev/sda"},
					Weight:  "1",
				},
				{
					Devices: []string{"/dev/sdb"},
					Weight:  "-2",
				},
			},
			expectedErrorCount: 3,
			expectedErrorSubstrings: []string{
				"(99999) bigger than maximum",
				"(1) smaller than minimum",
				"(-2) smaller than minimum",
			},
		},
		{
			name: "throttling without listing Devices",
			dps: []DevicesParameters{
				{
					ThrottleReadBps:   "100M",
					ThrottleWriteIOPS: "20k",
				},
			},
			expectedErrorCount: 1,
			expectedErrorSubstrings: []string{
				"Devices not listed",
				"\"100M\"",
				"\"20k\"",
			},
		},
	}
	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			oci, err := devicesParametersToCgBlockIO(tc.dps, tc.iosched)
			testutils.VerifyError(t, err, tc.expectedErrorCount, tc.expectedErrorSubstrings)
			if tc.expectedOci != nil {
				testutils.VerifyDeepEqual(t, "OCI parameters", *tc.expectedOci, oci)
			}
		})
	}
}

// mockPlatform implements mock versions of platformInterface functions.
type mockPlatform struct{}

// configurableBlockDevices mock always returns a set of block devices.
func (mpf mockPlatform) configurableBlockDevices(devWildcards []string) ([]tBlockDeviceInfo, error) {
	blockDevices := []tBlockDeviceInfo{}
	for _, devWildcard := range devWildcards {
		if devWildcard == "/dev/sda" {
			blockDevices = append(blockDevices, tBlockDeviceInfo{
				Major:   11,
				Minor:   12,
				DevNode: devWildcard,
				Origin:  fmt.Sprintf("from wildcards %v", devWildcard),
			})
		} else if devWildcard == "/dev/sdb" {
			blockDevices = append(blockDevices, tBlockDeviceInfo{
				Major:   21,
				Minor:   22,
				DevNode: devWildcard,
				Origin:  fmt.Sprintf("from wildcards %v", devWildcard),
			})
		} else if devWildcard == "/dev/sdc" {
			blockDevices = append(blockDevices, tBlockDeviceInfo{
				Major:   31,
				Minor:   32,
				DevNode: devWildcard,
				Origin:  fmt.Sprintf("from wildcards %v", devWildcard),
			})
		}
	}
	return blockDevices, nil
}
