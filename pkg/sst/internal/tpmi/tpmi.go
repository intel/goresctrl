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

package tpmi

import (
	"fmt"
	"math/bits"
	"unsafe"

	"github.com/intel/goresctrl/pkg/sst/internal/isst"
)

// numClos is the number of CLOSes supported by SST-CP.
const numClos = 4

// GetSocketPunits returns the valid TPMI power domain (punit) indices for a socket.
func GetSocketPunits(socketID uint8) ([]uint8, error) {
	req := isst.TpmiInstanceCount{Socket_id: socketID}
	if err := isst.Ioctl(isst.ISST_IF_COUNT_TPMI_INSTANCES, uintptr(unsafe.Pointer(&req))); err != nil {
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

// GetCPUPunitID returns the TPMI power domain (punit) ID for a given logical CPU.
func GetCPUPunitID(cpu uint16) (uint8, error) {
	physicalCPU, err := isst.GetPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return uint8((physicalCPU >> 11) & 0x1f), nil
}

// GetPunitCoreID returns the TPMI punit core id for a given logical CPU.
func GetPunitCoreID(cpu uint16) (uint16, error) {
	p, err := isst.GetPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return (p >> 3) & 0xff, nil
}

// PPGetPerfLevels reads performance level info for a punit.
func PPGetPerfLevels(socketID, punitID uint8) (isst.PerfLevelInfo, error) {
	info := isst.PerfLevelInfo{Socket_id: socketID, Power_domain_id: punitID}
	if err := isst.Ioctl(isst.ISST_IF_PERF_LEVELS, uintptr(unsafe.Pointer(&info))); err != nil {
		return isst.PerfLevelInfo{}, fmt.Errorf("ISST_IF_PERF_LEVELS for socket %d punit %d: %w", socketID, punitID, err)
	}
	return info, nil
}

// CPSetState sets SST-CP enabled/disabled with the given priority for one punit.
func CPSetState(socketID, punitID uint8, enable bool, priority uint8) error {
	cpState := isst.CorePower{
		Get_set:         1,
		Socket_id:       socketID,
		Power_domain_id: punitID,
		Priority_type:   uint8(priority),
	}
	if enable {
		cpState.Enable = 1
	}
	if err := isst.Ioctl(isst.ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
		return fmt.Errorf("ISST_IF_CORE_POWER_STATE set for socket %d punit %d: %w", socketID, punitID, err)
	}
	return nil
}

// CPSetPriorityType updates the CP priority type for one punit, preserving the current enable state.
func CPSetPriorityType(socketID, punitID uint8, priority uint8) error {
	cpState := isst.CorePower{Socket_id: socketID, Power_domain_id: punitID}
	if err := isst.Ioctl(isst.ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
		return fmt.Errorf("ISST_IF_CORE_POWER_STATE for socket %d punit %d: %w", socketID, punitID, err)
	}
	cpState.Get_set = 1
	cpState.Priority_type = priority
	if err := isst.Ioctl(isst.ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
		return fmt.Errorf("ISST_IF_CORE_POWER_STATE set for socket %d punit %d: %w", socketID, punitID, err)
	}
	return nil
}

// CPGetPriorityType reads the current CP priority type for one punit.
func CPGetPriorityType(socketID, punitID uint8) (uint8, error) {
	state, err := CPGetState(socketID, punitID)
	if err != nil {
		return 0, err
	}
	return state.Priority_type, nil
}

// CPGetState reads the SST-CP state for a punit.
func CPGetState(socketID, punitID uint8) (isst.CorePower, error) {
	cpState := isst.CorePower{Socket_id: socketID, Power_domain_id: punitID}
	if err := isst.Ioctl(isst.ISST_IF_CORE_POWER_STATE, uintptr(unsafe.Pointer(&cpState))); err != nil {
		return isst.CorePower{}, fmt.Errorf("ISST_IF_CORE_POWER_STATE for socket %d punit %d: %w", socketID, punitID, err)
	}
	return cpState, nil
}

// ClosSetParam writes CLOS parameters for one CLOS on one punit interface.
// Frequency values are in MHz.
func ClosSetParam(socketID, punitID, clos, proportional uint8, minFreq, maxFreq uint16) error {
	param := isst.ClosParam{
		Get_set:         1,
		Socket_id:       socketID,
		Power_domain_id: punitID,
		Clos:            clos,
		Prop_prio:       proportional,
		Min_freq_mhz:    minFreq,
		Max_freq_mhz:    maxFreq,
	}
	if err := isst.Ioctl(isst.ISST_IF_CLOS_PARAM, uintptr(unsafe.Pointer(&param))); err != nil {
		return fmt.Errorf("ISST_IF_CLOS_PARAM set for clos %d punit %d: %w", clos, punitID, err)
	}
	return nil
}

// ClosGetParam reads CLOS parameters for one CLOS on a punit.
func ClosGetParam(socketID, punitID, closIdx uint8) (isst.ClosParam, error) {
	closParam := isst.ClosParam{
		Socket_id:       socketID,
		Power_domain_id: punitID,
		Clos:            closIdx,
	}
	if err := isst.Ioctl(isst.ISST_IF_CLOS_PARAM, uintptr(unsafe.Pointer(&closParam))); err != nil {
		return isst.ClosParam{}, fmt.Errorf("ISST_IF_CLOS_PARAM for clos %d punit %d: %w", closIdx, punitID, err)
	}
	return closParam, nil
}

// ClosReset resets all CLOSes to default values for one punit.
func ClosReset(socketID, punitID uint8) error {
	for clos := 0; clos < numClos; clos++ {
		if err := ClosSetParam(socketID, punitID, uint8(clos), 0, 0, 25500); err != nil {
			return err
		}
	}
	return nil
}

// ClosAssociate associates a punit core to a CLOS.
func ClosAssociate(socketID, punitID uint8, punitCoreID uint16, clos uint8) error {
	req := isst.ClosAssocCmds{
		Cmd_count:     1,
		Get_set:       1,
		Punit_cpu_map: 1,
		Assoc_info: [1]isst.ClosAssoc{{
			Socket_id:       socketID,
			Power_domain_id: punitID,
			Logical_cpu:     punitCoreID,
			Clos:            uint16(clos),
		}},
	}
	if err := isst.Ioctl(isst.ISST_IF_CLOS_ASSOC, uintptr(unsafe.Pointer(&req))); err != nil {
		return fmt.Errorf("ISST_IF_CLOS_ASSOC set for socket %d punit %d core %d clos %d: %w", socketID, punitID, punitCoreID, clos, err)
	}
	return nil
}

// GetCPUClosID returns the CLOS id for a punit core.
func GetCPUClosID(socketID, punitID uint8, punitCoreID uint16) (uint8, error) {
	req := isst.ClosAssocCmds{
		Cmd_count:     1,
		Punit_cpu_map: 1,
		Assoc_info: [1]isst.ClosAssoc{{
			Socket_id:       socketID,
			Power_domain_id: punitID,
			Logical_cpu:     punitCoreID,
		}},
	}
	if err := isst.Ioctl(isst.ISST_IF_CLOS_ASSOC, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("ISST_IF_CLOS_ASSOC for socket %d punit %d core %d: %w", socketID, punitID, punitCoreID, err)
	}
	return uint8(req.Assoc_info[0].Clos), nil
}

// BFSetStatus enables or disables SST-BF for one punit, preserving the current TF state.
func BFSetStatus(socketID, punitID uint8, enable bool) error {
	perfInfo := isst.PerfLevelInfo{Socket_id: socketID, Power_domain_id: punitID}
	if err := isst.Ioctl(isst.ISST_IF_PERF_LEVELS, uintptr(unsafe.Pointer(&perfInfo))); err != nil {
		return fmt.Errorf("ISST_IF_PERF_LEVELS for socket %d punit %d: %w", socketID, punitID, err)
	}
	feature := perfInfo.Feature_state & 0x02 // preserve TF bit
	if enable {
		feature |= 0x01
	}
	ctrl := isst.PerfFeatureControl{Socket_id: socketID, Power_domain_id: punitID, Feature: feature}
	if err := isst.Ioctl(isst.ISST_IF_PERF_SET_FEATURE, uintptr(unsafe.Pointer(&ctrl))); err != nil {
		return fmt.Errorf("ISST_IF_PERF_SET_FEATURE (BF=%v) for socket %d punit %d: %w", enable, socketID, punitID, err)
	}
	return nil
}

// BFGetCoreMask reads the BF core mask for a punit and perf level.
func BFGetCoreMask(socketID, punitID, level uint8) (uint64, error) {
	cpuMask := isst.PerfLevelCpuMask{
		Socket_id:       socketID,
		Power_domain_id: punitID,
		Level:           level,
		Punit_cpu_map:   1,
	}
	if err := isst.Ioctl(isst.ISST_IF_GET_BASE_FREQ_CPU_MASK, uintptr(unsafe.Pointer(&cpuMask))); err != nil {
		return 0, fmt.Errorf("ISST_IF_GET_BASE_FREQ_CPU_MASK for socket %d punit %d: %w", socketID, punitID, err)
	}
	return cpuMask.Mask, nil
}

// TFSetStatus enables or disables SST-TF for one punit via the TPMI interface,
// preserving the current BF state.
func TFSetStatus(socketID, punitID uint8, enable bool) error {
	perfInfo := isst.PerfLevelInfo{Socket_id: socketID, Power_domain_id: punitID}
	if err := isst.Ioctl(isst.ISST_IF_PERF_LEVELS, uintptr(unsafe.Pointer(&perfInfo))); err != nil {
		return fmt.Errorf("ISST_IF_PERF_LEVELS for socket %d punit %d: %w", socketID, punitID, err)
	}
	feature := perfInfo.Feature_state & 0x01 // preserve BF bit
	if enable {
		feature |= 0x02
	}
	ctrl := isst.PerfFeatureControl{Socket_id: socketID, Power_domain_id: punitID, Feature: feature}
	if err := isst.Ioctl(isst.ISST_IF_PERF_SET_FEATURE, uintptr(unsafe.Pointer(&ctrl))); err != nil {
		return fmt.Errorf("ISST_IF_PERF_SET_FEATURE (TF=%v) for socket %d punit %d: %w", enable, socketID, punitID, err)
	}
	return nil
}

// PerfLevelGetCPUMask reads the punit core bitmask for a performance level.
func PerfLevelGetCPUMask(socketID, punitID, level uint8) (uint64, error) {
	cpuMask := isst.PerfLevelCpuMask{
		Socket_id:       socketID,
		Power_domain_id: punitID,
		Level:           level,
		Punit_cpu_map:   1,
	}
	if err := isst.Ioctl(isst.ISST_IF_GET_PERF_LEVEL_CPU_MASK, uintptr(unsafe.Pointer(&cpuMask))); err != nil {
		return 0, fmt.Errorf("ISST_IF_GET_PERF_LEVEL_CPU_MASK for socket %d punit %d level %d: %w", socketID, punitID, level, err)
	}
	return cpuMask.Mask, nil
}

// BFGetInfo reads SST-BF frequency info for a performance level.
func BFGetInfo(socketID, punitID, level uint8) (isst.BaseFreqInfo, error) {
	info := isst.BaseFreqInfo{Socket_id: socketID, Power_domain_id: punitID, Level: uint16(level)}
	if err := isst.Ioctl(isst.ISST_IF_GET_BASE_FREQ_INFO, uintptr(unsafe.Pointer(&info))); err != nil {
		return isst.BaseFreqInfo{}, fmt.Errorf("ISST_IF_GET_BASE_FREQ_INFO for socket %d punit %d level %d: %w", socketID, punitID, level, err)
	}
	return info, nil
}

// TFGetInfo reads SST-TF frequency info for a performance level.
func TFGetInfo(socketID, punitID, level uint8) (isst.TurboFreqInfo, error) {
	info := isst.TurboFreqInfo{Socket_id: socketID, Power_domain_id: punitID, Level: uint16(level)}
	if err := isst.Ioctl(isst.ISST_IF_GET_TURBO_FREQ_INFO, uintptr(unsafe.Pointer(&info))); err != nil {
		return isst.TurboFreqInfo{}, fmt.Errorf("ISST_IF_GET_TURBO_FREQ_INFO for socket %d punit %d level %d: %w", socketID, punitID, level, err)
	}
	return info, nil
}
