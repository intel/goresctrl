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
	"math/bits"
	"unsafe"

	"github.com/intel/goresctrl/pkg/utils"
)

// getPlatformAPIVersion gets the ISST API version.
func getPlatformAPIVersion() (int, error) {
	var info isstIfPlatformInfo
	if err := isstIoctl(ISST_IF_GET_PLATFORM_INFO, uintptr(unsafe.Pointer(&info))); err != nil {
		return 0, fmt.Errorf("failed to get ISST platform info: %w", err)
	}
	return int(info.Api_version), nil
}

// getSocketTpmiPunits returns the valid TPMI power domain (punit) indices for
// the given socket, in ascending order.
func getSocketTpmiPunits(socketID uint8) ([]uint8, error) {
	req := isstTpmiInstanceCount{Socket_id: socketID}
	if err := isstIoctl(ISST_IF_COUNT_TPMI_INSTANCES, uintptr(unsafe.Pointer(&req))); err != nil {
		return nil, fmt.Errorf("ISST_IF_COUNT_TPMI_INSTANCES for socket %d: %w", socketID, err)
	}
	if req.Valid_mask == 0 {
		return nil, fmt.Errorf("no valid TPMI instances for socket %d", socketID)
	}
	var punits []uint8
	for mask := req.Valid_mask; mask != 0; mask &= mask - 1 {
		punits = append(punits, uint8(bits.TrailingZeros16(mask)))
	}
	return punits, nil
}

// getCPUPunitID returns the TPMI power domain (punit) ID for a given logical CPU.
func getCPUPunitID(cpu uint16) (uint8, error) {
	physicalCPU, err := getPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return uint8((physicalCPU >> 11) & 0x1f), nil
}

// getPunitCoreIdMbox returns the PUNIT core id corresponding to a given Linux logical CPU for the TPMI interface.
func getPunitCoreIdTPMI(cpu uint16) (uint16, error) {
	p, err := getPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return (p >> 3) & 0xff, nil
}

// getSinglePackageInfoTPMI reads SST information for one CPU package using TPMI interface.
func getSinglePackageInfoTPMI(pkg *cpuPackageInfo) (SstPackageInfo, error) {
	info := SstPackageInfo{pkg: pkg}
	socketID := pkg.id

	punits, err := getSocketTpmiPunits(socketID)
	if err != nil {
		return info, err
	}
	info.tpmiPunits = punits
	// pkgPunit is the punit used for querying feature state, which we control on package level
	sstlog.Debug("TPMI punits for socket", "socket", socketID, "punits", fmt.Sprintf("%v", punits))
	pkgPunit := punits[0]

	perfInfo := isstPerfLevelInfo{
		Socket_id:       socketID,
		Power_domain_id: pkgPunit,
	}
	if err := isstIoctl(ISST_IF_PERF_LEVELS, uintptr(unsafe.Pointer(&perfInfo))); err != nil {
		return info, fmt.Errorf("ISST_IF_PERF_LEVELS for socket %d punit %d: %w", socketID, pkgPunit, err)
	}

	info.PPSupported = perfInfo.Enabled != 0
	info.PPLocked = perfInfo.Locked != 0
	info.PPVersion = int(perfInfo.Feature_rev)
	info.PPCurrentLevel = int(perfInfo.Current_level)
	info.PPMaxLevel = int(perfInfo.Max_level)

	info.BFSupported = perfInfo.Sst_bf_support != 0
	info.TFSupported = perfInfo.Sst_tf_support != 0

	info.BFEnabled = (perfInfo.Feature_state & 0x01) != 0
	info.TFEnabled = (perfInfo.Feature_state & 0x02) != 0

	// Read base-frequency info
	if info.BFSupported {
		info.BFCores = utils.IDSet{}

		for _, punit := range punits {
			// Build reverse map for CPUs in this punit: punit-core-ID -> logical CPUs.
			punitCoreIDs := make(map[uint16]utils.IDSet)
			for _, id := range pkg.cpus {
				cpuPunit, err := getCPUPunitID(id)
				if err != nil || cpuPunit != uint8(punit) {
					continue
				}
				punitCore, err := getPunitCoreIdTPMI(id)
				if err != nil {
					return info, fmt.Errorf("failed to get punit core for cpu %d: %w", id, err)
				}
				if _, ok := punitCoreIDs[punitCore]; !ok {
					punitCoreIDs[punitCore] = utils.IDSet{}
				}
				punitCoreIDs[punitCore].Add(utils.ID(id))
			}

			cpuMask := isstPerfLevelCpuMask{
				Socket_id:       socketID,
				Power_domain_id: punit,
				Level:           uint8(info.PPCurrentLevel),
				Punit_cpu_map:   1,
			}
			if err := isstIoctl(ISST_IF_GET_BASE_FREQ_CPU_MASK, uintptr(unsafe.Pointer(&cpuMask))); err != nil {
				return info, fmt.Errorf("ISST_IF_GET_BASE_FREQ_CPU_MASK for socket %d punit %d: %w", socketID, punit, err)
			}

			for mask := cpuMask.Mask; mask != 0; mask &= mask - 1 {
				bit := uint16(bits.TrailingZeros64(mask))
				if cpus, ok := punitCoreIDs[bit]; ok {
					info.BFCores.Add(cpus.Members()...)
				}
			}
		}
	}

	// Read core-power feature info
	cpState := isstCorePower{
		Socket_id:       socketID,
		Power_domain_id: pkgPunit,
	}
	if err := isstIoctl(ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
		return info, fmt.Errorf("ISST_IF_CORE_POWER_STATE for socket %d punit %d: %w", socketID, pkgPunit, err)
	}

	info.CPSupported = cpState.Supported != 0
	info.CPEnabled = cpState.Enable != 0
	info.CPPriority = CPPriorityType(cpState.Priority_type)

	if info.CPSupported {
		info.ClosCPUInfo = make(map[int]utils.IDSet, NumClos)

		for i := 0; i < NumClos; i++ {
			closParam := isstClosParam{
				Socket_id:       socketID,
				Power_domain_id: pkgPunit,
				Clos:            uint8(i),
			}
			if err := isstIoctl(ISST_IF_CLOS_PARAM, uintptr(unsafe.Pointer(&closParam))); err != nil {
				return info, fmt.Errorf("ISST_IF_CLOS_PARAM for clos %d: %w", i, err)
			}

			// TPMI reports frequencies in MHz; divide by 100 to match the
			// ratio-unit convention used by the mbox interface.
			info.ClosInfo[i] = SstClosInfo{
				ProportionalPriority: int(closParam.Prop_prio),
				MinFreq:              int(closParam.Min_freq_mhz) / 100,
				MaxFreq:              int(closParam.Max_freq_mhz) / 100,
			}
		}

		for _, id := range pkg.cpus {
			punitID, err := getCPUPunitID(id)
			if err != nil {
				sstlog.Debug("failed to get punit id for cpu", "cpu", id, "error", err)
				continue
			}
			closID, err := getCPUClosIDTPMI(id, socketID, punitID)
			if err != nil {
				sstlog.Debug("failed to get CLOS id for cpu", "cpu", id, "error", err)
				continue
			}
			if info.ClosCPUInfo[int(closID)] == nil {
				info.ClosCPUInfo[int(closID)] = utils.NewIDSet(utils.ID(id))
			} else {
				info.ClosCPUInfo[int(closID)].Add(utils.ID(id))
			}
		}
	}

	return info, nil
}

// getCPUClosIDTPMI returns the CLOS id for a CPU using the TPMI interface.
func getCPUClosIDTPMI(cpu uint16, socketID uint8, punitID uint8) (uint8, error) {
	punitCore, err := getPunitCoreIdTPMI(cpu)
	if err != nil {
		return 0, fmt.Errorf("invalid punit core for cpu %d: %w", cpu, err)
	}

	req := isstIfClosAssocCmds{
		Cmd_count:     1,
		Punit_cpu_map: 1,
		Assoc_info: [1]isstIfClosAssoc{{
			Socket_id:       socketID,
			Power_domain_id: punitID,
			Logical_cpu:     punitCore,
		}},
	}

	if err := isstIoctl(ISST_IF_CLOS_ASSOC, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("ISST_IF_CLOS_ASSOC for cpu %d: %w", cpu, err)
	}

	return uint8(req.Assoc_info[0].Clos), nil
}

// setBFStatusTPMI enables or disables SST-BF via the TPMI interface.
func setBFStatusTPMI(info *SstPackageInfo, status bool) error {
	feature := uint8(0)
	if status {
		feature |= 0x01
	}
	if info.TFEnabled {
		feature |= 0x02
	}
	for _, punit := range info.tpmiPunits {
		ctrl := isstPerfFeatureControl{
			Socket_id:       info.pkg.id,
			Power_domain_id: punit,
			Feature:         feature,
		}
		if err := isstIoctl(ISST_IF_PERF_SET_FEATURE, uintptr(unsafe.Pointer(&ctrl))); err != nil {
			return fmt.Errorf("ISST_IF_PERF_SET_FEATURE (BF=%v) for punit %d: %w", status, punit, err)
		}
	}
	info.BFEnabled = status
	return nil
}

// saveClosTPMI writes CLOS parameters for one CLOS via the TPMI interface.
// Frequency values in closInfo are in the mbox "ratio" units (×100 = MHz).
func saveClosTPMI(info *SstPackageInfo, clos uint8, closInfo *SstClosInfo) error {
	for _, punit := range info.tpmiPunits {
		param := isstClosParam{
			Get_set:         1,
			Socket_id:       info.pkg.id,
			Power_domain_id: punit,
			Clos:            clos,
			Prop_prio:       uint8(closInfo.ProportionalPriority),
			// Convert from getPunitCoreIdMboxck to MHz for the TPMI kernel interface.
			Min_freq_mhz: uint16(closInfo.MinFreq * 100),
			Max_freq_mhz: uint16(closInfo.MaxFreq * 100),
		}
		if err := isstIoctl(ISST_IF_CLOS_PARAM, uintptr(unsafe.Pointer(&param))); err != nil {
			return fmt.Errorf("ISST_IF_CLOS_PARAM set for clos %d punit %d: %w", clos, punit, err)
		}
	}
	return nil
}

// associate2ClosTPMI associates one CPU to a CLOS via the TPMI interface.
func associate2ClosTPMI(cpu uint16, socketID uint8, clos uint8) error {
	punitCore, err := getPunitCoreIdTPMI(cpu)
	if err != nil {
		return fmt.Errorf("invalid punit core for cpu %d: %w", cpu, err)
	}

	punitID, err := getCPUPunitID(cpu)
	if err != nil {
		return fmt.Errorf("invalid punit id for cpu %d: %w", cpu, err)
	}

	req := isstIfClosAssocCmds{
		Cmd_count:     1,
		Get_set:       1,
		Punit_cpu_map: 1,
		Assoc_info: [1]isstIfClosAssoc{{
			Socket_id:       socketID,
			Power_domain_id: punitID,
			Logical_cpu:     punitCore,
			Clos:            uint16(clos),
		}},
	}

	if err := isstIoctl(ISST_IF_CLOS_ASSOC, uintptr(unsafe.Pointer(&req))); err != nil {
		return fmt.Errorf("ISST_IF_CLOS_ASSOC set for cpu %d clos %d: %w", cpu, clos, err)
	}
	return nil
}

// setCPStateTPMI sets SST-CP enabled/disabled for all valid punits in the
// package via the TPMI interface. CP is a per-package setting that must be
// applied to every punit.
func setCPStateTPMI(info *SstPackageInfo, enable bool) error {
	for _, punit := range info.tpmiPunits {
		cpState := isstCorePower{
			Get_set:         1,
			Socket_id:       info.pkg.id,
			Power_domain_id: punit,
			Priority_type:   uint8(info.CPPriority),
		}
		if enable {
			cpState.Enable = 1
		}
		if err := isstIoctl(ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
			return fmt.Errorf("ISST_IF_CORE_POWER_STATE set for socket %d punit %d: %w", info.pkg.id, punit, err)
		}
	}

	info.CPEnabled = enable
	return nil
}

// enableCPTPMI enables SST-CP for the package via the TPMI interface.
func enableCPTPMI(info *SstPackageInfo) error {
	return setCPStateTPMI(info, true)
}

// disableCPTPMI disables SST-CP for the package via the TPMI interface.
func disableCPTPMI(info *SstPackageInfo) error {
	return setCPStateTPMI(info, false)
}

// setDefaultClosParamTPMI resets all CLOSes to default values via the TPMI interface.
func setDefaultClosParamTPMI(info *SstPackageInfo) error {
	defaultConfig := &SstClosInfo{MaxFreq: 255}

	for clos := 0; clos < NumClos; clos++ {
		if err := saveClosTPMI(info, uint8(clos), defaultConfig); err != nil {
			return err
		}
	}
	return nil
}
