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
	"testing"

	oci "github.com/opencontainers/runtime-spec/specs-go"

	"github.com/intel/goresctrl/pkg/cgroups"
	"github.com/intel/goresctrl/pkg/testutils"
)

// TestOciLinuxBlockIO: unit tests for OciLinuxBlockIO().
func TestOciLinuxBlockIO(t *testing.T) {
	tcases := []struct {
		name            string
		class           string
		blockIOClasses  map[string]cgroups.BlockIOParameters
		expectedBlockIO *oci.LinuxBlockIO
		// It would be great to define expected
		// oci.LinuxBlockIO with a single literal.  But that
		// is impossible because Major and Minor fields are
		// inside non-exported oci.linuxBlockIODevice
		// struct. Therefore here are expected triplets of
		// major/minor/(weight|rate).
		expectedWeight                   uint16
		expectedWeightDevices            [][3]uint16
		expectedThrottleReadBpsDevices   [][3]uint64
		expectedThrottleWriteBpsDevices  [][3]uint64
		expectedThrottleReadIOPSDevices  [][3]uint64
		expectedThrottleWriteIOPSDevices [][3]uint64
		expectedErrorSubstrings          []string
	}{
		{
			name:                    "unknown class",
			class:                   "foobar",
			blockIOClasses:          nil,
			expectedErrorSubstrings: []string{"foobar"},
		},
		{
			name:  "all fields",
			class: "allfields",
			blockIOClasses: map[string]cgroups.BlockIOParameters{
				"allfields": cgroups.BlockIOParameters{
					Weight: 10,
					WeightDevice: cgroups.DeviceWeights{
						{Major: 20, Minor: 21, Weight: 22},
						{Major: 23, Minor: 24, Weight: 25},
					},
					ThrottleReadBpsDevice: cgroups.DeviceRates{
						{Major: 30, Minor: 31, Rate: 32},
						{Major: 33, Minor: 34, Rate: 35},
					},
					ThrottleWriteBpsDevice: cgroups.DeviceRates{
						{Major: 40, Minor: 41, Rate: 42},
						{Major: 43, Minor: 44, Rate: 45},
					},
					ThrottleReadIOPSDevice: cgroups.DeviceRates{
						{Major: 50, Minor: 51, Rate: 52},
						{Major: 53, Minor: 54, Rate: 55},
					},
					ThrottleWriteIOPSDevice: cgroups.DeviceRates{
						{Major: 60, Minor: 61, Rate: 62},
						{Major: 63, Minor: 64, Rate: 65},
					},
				},
			},
			expectedWeight:                   10,
			expectedWeightDevices:            [][3]uint16{{20, 21, 22}, {23, 24, 25}},
			expectedThrottleReadBpsDevices:   [][3]uint64{{30, 31, 32}, {33, 34, 35}},
			expectedThrottleWriteBpsDevices:  [][3]uint64{{40, 41, 42}, {43, 44, 45}},
			expectedThrottleReadIOPSDevices:  [][3]uint64{{50, 51, 52}, {53, 54, 55}},
			expectedThrottleWriteIOPSDevices: [][3]uint64{{60, 61, 62}, {63, 64, 65}},
			expectedBlockIO:                  &oci.LinuxBlockIO{},
		},
	}
	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			classBlockIO = tc.blockIOClasses
			gotBlockIO, gotError := OciLinuxBlockIO(tc.class)
			expectedErrorCount := 0
			if len(tc.expectedErrorSubstrings) > 0 {
				expectedErrorCount = 1
			}
			testutils.VerifyError(t, gotError, expectedErrorCount, tc.expectedErrorSubstrings)
			if tc.expectedBlockIO != nil {
				tc.expectedBlockIO.Weight = &tc.expectedWeight
				for _, wd := range tc.expectedWeightDevices {
					tc.expectedBlockIO.WeightDevice = append(tc.expectedBlockIO.WeightDevice, linuxWeightDevice(wd))
				}
				for _, rd := range tc.expectedThrottleReadBpsDevices {
					tc.expectedBlockIO.ThrottleReadBpsDevice = append(tc.expectedBlockIO.ThrottleReadBpsDevice, linuxThrottleDevice(rd))
				}
				for _, rd := range tc.expectedThrottleWriteBpsDevices {
					tc.expectedBlockIO.ThrottleWriteBpsDevice = append(tc.expectedBlockIO.ThrottleWriteBpsDevice, linuxThrottleDevice(rd))
				}
				for _, rd := range tc.expectedThrottleReadIOPSDevices {
					tc.expectedBlockIO.ThrottleReadIOPSDevice = append(tc.expectedBlockIO.ThrottleReadIOPSDevice, linuxThrottleDevice(rd))
				}
				for _, rd := range tc.expectedThrottleWriteIOPSDevices {
					tc.expectedBlockIO.ThrottleWriteIOPSDevice = append(tc.expectedBlockIO.ThrottleWriteIOPSDevice, linuxThrottleDevice(rd))
				}
			}
			testutils.VerifyDeepEqual(t, "OCI BlockIO", tc.expectedBlockIO, gotBlockIO)
		})
	}
}

func linuxWeightDevice(triplet [3]uint16) oci.LinuxWeightDevice {
	wd := oci.LinuxWeightDevice{}
	wd.Major = int64(triplet[0])
	wd.Minor = int64(triplet[1])
	wd.Weight = &triplet[2]
	return wd
}

func linuxThrottleDevice(triplet [3]uint64) oci.LinuxThrottleDevice {
	rd := oci.LinuxThrottleDevice{}
	rd.Major = int64(triplet[0])
	rd.Minor = int64(triplet[1])
	rd.Rate = triplet[2]
	return rd
}
