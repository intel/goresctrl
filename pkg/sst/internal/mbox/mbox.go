/*
Copyright 2021-2026 Intel Corporation

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

package mbox

import (
	"fmt"

	"github.com/intel/goresctrl/pkg/sst/internal/isst"
)

// numClos is the number of CLOSes supported by SST-CP.
const numClos = 4

// PPGetCurrentLevel reads the current PP level for a CPU.
func PPGetCurrentLevel(cpu uint16) (int, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_LEVELS_INFO, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read SST PP info: %w", err)
	}
	return int(getBits(rsp, 16, 23)), nil
}

// PPInfo holds SST-PP (perf profile) information.
type PPInfo struct {
	Supported    bool
	Locked       bool
	CurrentLevel int
	MaxLevel     int
	Version      int
}

// PPReadInfo reads SST-PP info for a CPU.
func PPReadInfo(cpu uint16) (PPInfo, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_LEVELS_INFO, 0, 0)
	if err != nil {
		return PPInfo{}, fmt.Errorf("failed to read SST PP info: %v", err)
	}
	return PPInfo{
		Supported:    getBits(rsp, 31, 31) != 0,
		Locked:       getBits(rsp, 24, 24) != 0,
		CurrentLevel: int(getBits(rsp, 16, 23)),
		MaxLevel:     int(getBits(rsp, 8, 15)),
		Version:      int(getBits(rsp, 0, 7)),
	}, nil
}

// TDPControl holds SST-BF and SST-TF support/enable flags.
type TDPControl struct {
	BFSupported bool
	BFEnabled   bool
	TFSupported bool
	TFEnabled   bool
}

// PPReadTDPControl reads SST BF/TF status for a CPU at the given PP level.
func PPReadTDPControl(cpu uint16, ppCurrentLevel int) (TDPControl, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(ppCurrentLevel))
	if err != nil {
		return TDPControl{}, fmt.Errorf("failed to read SST BF/TF status: %v", err)
	}
	return TDPControl{
		BFSupported: isBitSet(rsp, 1),
		BFEnabled:   isBitSet(rsp, 17),
		TFSupported: isBitSet(rsp, 0),
		TFEnabled:   isBitSet(rsp, 16),
	}, nil
}

// getPunitCoreID returns the PUNIT core id for a CPU via mbox bit encoding.
func getPunitCoreID(cpu uint16) (uint16, error) {
	p, err := isst.GetPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return (p & 0x1ff) >> 1, nil
}

func writePMConfig(cpu uint16, enable bool) (uint32, error) {
	var req uint32
	if enable {
		req = setBit(0, 16)
	}
	if _, err := isst.SendMboxCmd(cpu, isst.WRITE_PM_CONFIG, isst.PM_FEATURE, 0, req); err != nil {
		return 0, fmt.Errorf("failed to set SST-CP status: %v", err)
	}
	rsp, err := isst.SendMboxCmd(cpu, isst.READ_PM_CONFIG, isst.PM_FEATURE, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to get SST-CP status: %v", err)
	}
	return rsp, nil
}

func writeClosPmQosConfig(cpu uint16, priority int, enable bool) error {
	var req uint32
	param := setBit(0, isst.MBOX_CMD_WRITE_BIT)
	if enable {
		req = setBit(0, 1)
		if priority > 0 {
			req = setBit(req, 2)
		}
	}
	if _, err := isst.SendMboxCmd(cpu, isst.CONFIG_CLOS, isst.CLOS_PM_QOS_CONFIG, param, req); err != nil {
		return fmt.Errorf("failed to set SST-CP status: %v", err)
	}
	return nil
}

// CPSetStatus sets the SST-CP status.
// Returns an error if the hardware did not reflect the expected state.
func CPSetStatus(cpu uint16, enable bool, priority int) error {
	if err := writeClosPmQosConfig(cpu, priority, enable); err != nil {
		return fmt.Errorf("cannot set Clos status: %v", err)
	}
	rsp, err := writePMConfig(cpu, enable)
	if err != nil {
		return err
	}
	if !isBitSet(rsp, 0) {
		return fmt.Errorf("SST-CP not supported (PM config word: %#x)", rsp)
	}
	if enable && !isBitSet(rsp, 16) {
		return fmt.Errorf("SST-CP enable not reflected in hardware (PM config word: %#x)", rsp)
	}
	if !enable && isBitSet(rsp, 16) {
		return fmt.Errorf("SST-CP disable not reflected in hardware (PM config word: %#x)", rsp)
	}
	return nil
}

// CPSetPriorityType changes the CP priority type.
func CPSetPriorityType(cpu uint16, priority int) error {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_CLOS, isst.CLOS_PM_QOS_CONFIG, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to read CLOS PM QoS: %w", err)
	}
	return writeClosPmQosConfig(cpu, priority, isBitSet(rsp, 1))
}

// CPStatus holds SST-CP support/enable status.
type CPStatus struct {
	Supported bool
	Enabled   bool
}

// CPReadStatus reads SST-CP support and enable state.
func CPReadStatus(cpu uint16) (CPStatus, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.READ_PM_CONFIG, isst.PM_FEATURE, 0, 0)
	if err != nil {
		return CPStatus{}, fmt.Errorf("failed to read SST CP info: %v", err)
	}
	return CPStatus{
		Supported: isBitSet(rsp, 0),
		Enabled:   isBitSet(rsp, 16),
	}, nil
}

// CPGetPriorityType reads the current CP priority type (0=proportional, 1=ordered).
func CPGetPriorityType(cpu uint16) (int, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_CLOS, isst.CLOS_PM_QOS_CONFIG, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read SST CP status: %v", err)
	}
	return int(getBits(rsp, 2, 2)), nil
}

// CPSendClosCmd sends a CLOS command via MMIO to PUNIT.
func CPSendClosCmd(cpu uint16, subCmd uint16, parameter uint32, reqData uint32) (uint32, error) {
	var id, offset uint32
	switch subCmd {
	case isst.CLOS_PQR_ASSOC:
		id = parameter & 0xff
		offset = isst.PQR_ASSOC_OFFSET
	case isst.CLOS_PM_CLOS:
		id = parameter & 0x03
		offset = isst.PM_CLOS_OFFSET
	case isst.CLOS_STATUS:
		fallthrough
	default:
		return 0, nil
	}
	return isst.SendMMIOCmd(cpu, (id<<2)+offset, reqData, isBitSet(parameter, isst.MBOX_CMD_WRITE_BIT))
}

// ClosSetParam writes CLOS parameters for one CLOS. Frequency values are 8-bit ratios (x100 = MHz).
func ClosSetParam(cpu uint16, clos, epp, proportional, minFreq, maxFreq, desiredFreq uint8) error {
	req := uint32(epp & 0x0f)
	req |= uint32((proportional)&0x0f) << 4
	req |= uint32(minFreq) << 8
	req |= uint32(maxFreq) << 16
	req |= uint32(desiredFreq) << 24
	param := setBit(uint32(clos), isst.MBOX_CMD_WRITE_BIT)
	if _, err := CPSendClosCmd(cpu, isst.CLOS_PM_CLOS, param, req); err != nil {
		return fmt.Errorf("failed to save Clos: %v", err)
	}
	return nil
}

// ClosParam holds per-CLOS parameters read from hardware.
type ClosParam struct {
	Proportional int
	MinFreq      int
	MaxFreq      int
	Epp          int
	DesiredFreq  int
}

// ClosGetParam reads CLOS parameters for one CLOS index.
func ClosGetParam(cpu uint16, closIdx int) (ClosParam, error) {
	rsp, err := CPSendClosCmd(cpu, isst.CLOS_PM_CLOS, uint32(closIdx), 0)
	if err != nil {
		return ClosParam{}, fmt.Errorf("failed to read SST CLOS #%d info: %v", closIdx, err)
	}
	return ClosParam{
		Epp:          int(getBits(rsp, 0, 3)),
		Proportional: int(getBits(rsp, 4, 7)),
		MinFreq:      int(getBits(rsp, 8, 15)),
		MaxFreq:      int(getBits(rsp, 16, 23)),
		DesiredFreq:  int(getBits(rsp, 24, 31)),
	}, nil
}

// ClosReset resets all CLOSes to default values.
func ClosReset(cpu uint16) error {
	for clos := 0; clos < numClos; clos++ {
		if err := ClosSetParam(cpu, uint8(clos), 0, 0, 0, 255, 0); err != nil {
			return err
		}
	}
	return nil
}

// ClosAssociate associates one CPU to a CLOS.
func ClosAssociate(cpu uint16, clos uint8) error {
	coreID, err := getPunitCoreID(cpu)
	if err != nil {
		return fmt.Errorf("invalid core id for cpu %d: %v", cpu, err)
	}
	req := uint32(clos&0x03) << 16
	param := setBit(uint32(coreID), isst.MBOX_CMD_WRITE_BIT)
	if _, err := CPSendClosCmd(cpu, isst.CLOS_PQR_ASSOC, param, req); err != nil {
		return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
	}
	return nil
}

// GetCPUClosID returns the CLOS id of a CPU.
func GetCPUClosID(cpu uint16) (uint8, error) {
	punitCore, err := getPunitCoreID(cpu)
	if err != nil {
		return 0, fmt.Errorf("invalid core id for cpu %d: %v", cpu, err)
	}
	rsp, err := CPSendClosCmd(cpu, isst.CLOS_PQR_ASSOC, uint32(punitCore), 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read CLOS number of cpu %d: %v", cpu, err)
	}
	return uint8(getBits(rsp, 16, 17)), nil
}

// BFSetStatus enables or disables SST-BF.
func BFSetStatus(cpu uint16, ppCurrentLevel int, enable bool) error {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(ppCurrentLevel))
	if err != nil {
		return fmt.Errorf("failed to read SST status: %w", err)
	}
	req := clearBit(rsp, 17)
	if enable {
		req = setBit(rsp, 17)
	}
	if _, err = isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_SET_TDP_CONTROL, 0, req); err != nil {
		return fmt.Errorf("failed to set SST BF: %w", err)
	}
	return nil
}

// BFReadCoreMask reads one BF core mask word (32-bit) for the given PP level and mask index.
func BFReadCoreMask(cpu uint16, ppCurrentLevel, maskIndex int) (uint32, error) {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_PBF_GET_CORE_MASK_INFO, 0, uint32(ppCurrentLevel+(maskIndex<<8)))
	if err != nil {
		return 0, fmt.Errorf("failed to read SST BF core mask (#%d): %v", maskIndex, err)
	}
	return rsp, nil
}

// TFSetStatus enables or disables SST-TF via the Mbox interface.
func TFSetStatus(cpu uint16, ppCurrentLevel int, enable bool) error {
	rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(ppCurrentLevel))
	if err != nil {
		return fmt.Errorf("failed to read SST status: %w", err)
	}
	req := clearBit(rsp, 16)
	if enable {
		req = setBit(rsp, 16)
	}
	if _, err = isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_SET_TDP_CONTROL, 0, req); err != nil {
		return fmt.Errorf("failed to set SST TF: %w", err)
	}
	return nil
}

// PerfLevelGetCoreMask64 reads the 64-bit punit core bitmask for a PP level.
func PerfLevelGetCoreMask64(cpu uint16, level int) (uint64, error) {
	lo, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_CORE_MASK, 0, uint32(level))
	if err != nil {
		return 0, fmt.Errorf("failed to read level core mask (lo) at level %d: %w", level, err)
	}
	hi, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_CORE_MASK, 0, uint32(level|(1<<8)))
	if err != nil {
		return 0, fmt.Errorf("failed to read level core mask (hi) at level %d: %w", level, err)
	}
	return uint64(lo) | (uint64(hi) << 32), nil
}

// BFInfo holds SST-BF properties for one PP level.
type BFInfo struct {
	HighPriorityBaseFreqRatio int // ratio (x100 = MHz)
	LowPriorityBaseFreqRatio  int // ratio (x100 = MHz)
	CoreMask                  uint64
}

// BFGetInfo reads SST-BF properties for one PP level.
func BFGetInfo(cpu uint16, level int) (BFInfo, error) {
	p1, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_PBF_GET_P1HI_P1LO_INFO, 0, uint32(level))
	if err != nil {
		return BFInfo{}, fmt.Errorf("failed to read BF P1HI/P1LO at level %d: %w", level, err)
	}
	lo, err := BFReadCoreMask(cpu, level, 0)
	if err != nil {
		return BFInfo{}, err
	}
	hi, err := BFReadCoreMask(cpu, level, 1)
	if err != nil {
		return BFInfo{}, err
	}
	return BFInfo{
		HighPriorityBaseFreqRatio: int(getBits(p1, 8, 15)),
		LowPriorityBaseFreqRatio:  int(getBits(p1, 0, 7)),
		CoreMask:                  uint64(lo) | (uint64(hi) << 32),
	}, nil
}

// TFInfo holds SST-TF properties for one PP level.
// Note: Mbox TF only exposes up to 3 TRL levels (SSE=0, AVX2=1, AVX512=2).
type TFInfo struct {
	LPClipRatios [3]int    // x100 = MHz, indices 0=SSE, 1=AVX2, 2=AVX512
	HPCoreCounts [8]int    // high-priority core count per bucket
	HPTRLRatios  [3][8]int // [trllevel][bucket] ratio, x100 = MHz
}

// TFGetInfo reads SST-TF properties for one PP level.
func TFGetInfo(cpu uint16, level int) (TFInfo, error) {
	var d TFInfo

	// LP clip ratios (3 ISA levels packed in one response)
	lp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_FACT_LP_CLIPPING_RATIO, 0, uint32(level))
	if err != nil {
		return d, fmt.Errorf("failed to read TF LP clip ratios at level %d: %w", level, err)
	}
	d.LPClipRatios[0] = int(getBits(lp, 0, 7))
	d.LPClipRatios[1] = int(getBits(lp, 8, 15))
	d.LPClipRatios[2] = int(getBits(lp, 16, 23))

	// HP core counts (8 buckets, 4 per read)
	for i := range 2 {
		rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_FACT_HP_TURBO_LIMIT_NUMCORES, 0,
			uint32(level|(i<<8)))
		if err != nil {
			return d, fmt.Errorf("failed to read TF HP core counts at level %d: %w", level, err)
		}
		for j := range 4 {
			d.HPCoreCounts[i*4+j] = int(getBits(rsp, uint32(j*8), uint32(j*8+7)))
		}
	}

	// HP TRL ratios for each TRL level
	for k := range 3 {
		for i := range 2 {
			rsp, err := isst.SendMboxCmd(cpu, isst.CONFIG_TDP, isst.CONFIG_TDP_GET_FACT_HP_TURBO_LIMIT_RATIOS, 0,
				uint32(level|(i<<8)|(k<<16)))
			if err != nil {
				return d, fmt.Errorf("failed to read TF HP TRL ratios for perf level %d TRL level %d: %w", level, k, err)
			}
			for j := range 4 {
				d.HPTRLRatios[k][i*4+j] = int(getBits(rsp, uint32(j*8), uint32(j*8+7)))
			}
		}
	}
	return d, nil
}

func getBits(val, i, j uint32) uint32 {
	lsb := i
	msb := j
	if i > j {
		lsb = j
		msb = i
	}
	return (val >> lsb) & ((1 << (msb - lsb + 1)) - 1)
}

// isBitSet reports whether bit n is set in val.
func isBitSet(val, n uint32) bool {
	return val&(1<<n) != 0
}

// setBit sets bit n in val.
func setBit(val, n uint32) uint32 {
	return val | (1 << n)
}

// clearBit clears bit n in val.
func clearBit(val, n uint32) uint32 {
	return val &^ (1 << n)
}
