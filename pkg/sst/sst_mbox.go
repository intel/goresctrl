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
)

// getPunitCoreIdMbox returns the PUNIT core id corresponding to a given Linux logical CPU for the Mbox interface.
func getPunitCoreIdMbox(cpu uint16) (uint16, error) {
	p, err := getPunitCPUId(cpu)
	if err != nil {
		return 0, err
	}
	return (p & 0x1ff) >> 1, nil
}

// getSinglePackageInfoMbox reads SST information for one CPU package via the Mbox interface.
func getSinglePackageInfoMbox(pkg *cpuPackageInfo) (SstPackageInfo, error) {
	info := SstPackageInfo{}

	cpu := pkg.cpus[0]

	var err error
	var rsp uint32

	// Read perf-profile feature info
	if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_GET_LEVELS_INFO, 0, 0); err != nil {
		return info, fmt.Errorf("failed to read SST PP info: %v", err)
	}
	info.PPSupported = getBits(rsp, 31, 31) != 0
	info.PPLocked = getBits(rsp, 24, 24) != 0
	info.PPCurrentLevel = int(getBits(rsp, 16, 23))
	info.PPMaxLevel = int(getBits(rsp, 8, 15))
	info.PPVersion = int(getBits(rsp, 0, 7))
	info.pkg = pkg

	// Forget about older hw with partial/convoluted support
	if info.PPVersion < 3 {
		sstlog.Info("SST PP version less than 3, giving up...", "version", info.PPVersion)
		return info, nil
	}

	// Read the status of currently active perf-profile
	if !info.PPSupported {
		sstlog.Debug("SST PP feature not supported, only current profile level is valid", "profileLevel", info.PPCurrentLevel)
	}

	if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(info.PPCurrentLevel)); err != nil {
		return info, fmt.Errorf("failed to read SST BF/TF status: %v", err)
	}

	info.BFSupported = isBitSet(rsp, 1)
	info.BFEnabled = isBitSet(rsp, 17)

	info.TFSupported = isBitSet(rsp, 0)
	info.TFEnabled = isBitSet(rsp, 16)

	// Read base-frequency info
	if info.BFSupported {
		info.BFCores = utils.IDSet{}

		punitCoreIDs := make(map[uint16]utils.IDSet, len(pkg.cpus))
		var maxPunitCore uint16
		for _, id := range pkg.cpus {
			pc, err := getPunitCPUId(id)
			if err != nil {
				return info, err
			}
			punitCore := pc >> 1
			if _, ok := punitCoreIDs[punitCore]; !ok {
				punitCoreIDs[punitCore] = utils.IDSet{}
			}
			punitCoreIDs[punitCore].Add(utils.ID(id))
			if punitCore > maxPunitCore {
				maxPunitCore = punitCore
			}
		}

		// Read out core masks in batches of 32 (32 bits per response)
		for i := 0; i <= int(maxPunitCore)/32; i++ {
			if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_PBF_GET_CORE_MASK_INFO, 0, uint32(info.PPCurrentLevel+(i<<8))); err != nil {
				return info, fmt.Errorf("failed to read SST BF core mask (#%d): %v", i, err)
			}
			for bit := 0; bit < 32; bit++ {
				if isBitSet(rsp, uint32(bit)) {
					info.BFCores.Add(punitCoreIDs[uint16(i*32+bit)].Members()...)
				}
			}
		}
	}

	// Read core-power feature info
	if rsp, err = sendMboxCmd(cpu, READ_PM_CONFIG, PM_FEATURE, 0, 0); err != nil {
		return info, fmt.Errorf("failed to read SST CP info: %v", err)
	}

	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)

	if info.CPSupported {
		if rsp, err = sendMboxCmd(cpu, CONFIG_CLOS, CLOS_PM_QOS_CONFIG, 0, 0); err != nil {
			return info, fmt.Errorf("failed to read SST CP status: %v", err)
		}

		info.CPPriority = CPPriorityType(getBits(rsp, 2, 2))
		info.ClosCPUInfo = make(map[int]utils.IDSet, NumClos)

		for i := 0; i < NumClos; i++ {
			if rsp, err = sendClosCmd(cpu, CLOS_PM_CLOS, uint32(i), 0); err != nil {
				return info, fmt.Errorf("failed to read SST CLOS #%d info: %v", i, err)
			}

			info.ClosInfo[i] = SstClosInfo{
				EPP:                  int(getBits(rsp, 0, 3)),
				ProportionalPriority: int(getBits(rsp, 4, 7)),
				MinFreq:              int(getBits(rsp, 8, 15)),
				MaxFreq:              int(getBits(rsp, 16, 23)),
				DesiredFreq:          int(getBits(rsp, 24, 31)),
			}
		}

		for _, id := range pkg.cpus {
			closId, err := getCPUClosIDMbox(id)
			if err != nil {
				continue
			}

			if info.ClosCPUInfo[int(closId)] == nil {
				info.ClosCPUInfo[int(closId)] = utils.NewIDSet(utils.ID(id))
			} else {
				info.ClosCPUInfo[int(closId)].Add(utils.ID(id))
			}
		}
	}

	return info, nil
}

// getCPUClosIDMbox returns the CLOS id of a cpu using the Mbox interface.
func getCPUClosIDMbox(cpu uint16) (uint8, error) {
	punitCore, err := getPunitCoreIdMbox(cpu)
	if err != nil {
		return 0, fmt.Errorf("invalid core id for cpu %d: %v", cpu, err)
	}

	rsp, err := sendClosCmd(cpu, CLOS_PQR_ASSOC, uint32(punitCore), 0)
	if err != nil {
		return 0, fmt.Errorf("failed to read CLOS number of cpu %d: %v", cpu, err)
	}
	return uint8(getBits(rsp, 16, 17)), nil
}

// setBFStatusMbox enables or disables SST-BF via the Mbox interface.
func setBFStatusMbox(info *SstPackageInfo, status bool) error {
	rsp, err := sendMboxCmd(info.pkgCPU(), CONFIG_TDP, CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(info.PPCurrentLevel))
	if err != nil {
		return fmt.Errorf("failed to read SST status: %w", err)
	}

	req := clearBit(rsp, 17)
	if status {
		req = setBit(rsp, 17)
	}

	if _, err = sendMboxCmd(info.pkgCPU(), CONFIG_TDP, CONFIG_TDP_SET_TDP_CONTROL, 0, req); err != nil {
		return fmt.Errorf("failed to enable SST %s: %w", "BF", err)
	}

	info.BFEnabled = status

	return nil
}

// sendClosCmd sends a CLOS command via MMIO to PUNIT.
func sendClosCmd(cpu uint16, subCmd uint16, parameter uint32, reqData uint32) (uint32, error) {
	var id, offset uint32

	switch subCmd {
	case CLOS_PQR_ASSOC:
		id = parameter & 0xff // core id
		offset = PQR_ASSOC_OFFSET
	case CLOS_PM_CLOS:
		id = parameter & 0x03 // clos id
		offset = PM_CLOS_OFFSET
	case CLOS_STATUS:
		fallthrough
	default:
		return 0, nil
	}

	return sendMMIOCmd(cpu, (id<<2)+offset, reqData, isBitSet(parameter, MBOX_CMD_WRITE_BIT))
}

// saveClosMbox writes CLOS parameters for one CLOS via the Mbox interface.
func saveClosMbox(closInfo *SstClosInfo, cpu uint16, clos uint8) error {
	req := closInfo.EPP & 0x0f
	req |= (closInfo.ProportionalPriority & 0x0f) << 4
	req |= (closInfo.MinFreq & 0xff) << 8
	req |= (closInfo.MaxFreq & 0xff) << 16
	req |= (closInfo.DesiredFreq & 0xff) << 24

	param := setBit(uint32(clos), MBOX_CMD_WRITE_BIT)

	if _, err := sendClosCmd(cpu, CLOS_PM_CLOS, param, uint32(req)); err != nil {
		return fmt.Errorf("failed to save Clos: %v", err)
	}

	return nil
}

// associate2ClosMbox associates one CPU to a CLOS via the Mbox interface.
func associate2ClosMbox(cpu uint16, clos uint8) error {
	coreId, err := getPunitCoreIdMbox(cpu)
	if err != nil {
		return fmt.Errorf("invalid core id for cpu %d: %v", cpu, err)
	}

	req := uint32(clos&0x03) << 16
	param := setBit(uint32(coreId), MBOX_CMD_WRITE_BIT)

	if _, err := sendClosCmd(cpu, CLOS_PQR_ASSOC, param, uint32(req)); err != nil {
		return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
	}

	return nil
}

// writePMConfig writes the PM config to PUNIT via the Mbox interface and
// returns the updated config as read back.
func writePMConfig(cpu uint16, enable bool) (uint32, error) {
	var req uint32

	if enable {
		req = setBit(0, 16)
	}

	if _, err := sendMboxCmd(cpu, WRITE_PM_CONFIG, PM_FEATURE, 0, req); err != nil {
		return 0, fmt.Errorf("failed to set SST-CP status: %v", err)
	}

	rsp, err := sendMboxCmd(cpu, READ_PM_CONFIG, PM_FEATURE, 0, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to get SST-CP status: %v", err)
	}

	return rsp, nil
}

// writeClosPmQosConfig writes the CLOS PM QoS config via the Mbox interface.
func writeClosPmQosConfig(info *SstPackageInfo, cpu uint16, enable bool) error {
	var req uint32

	param := setBit(0, MBOX_CMD_WRITE_BIT)

	if enable {
		req = setBit(0, 1)

		if info.CPPriority > 0 {
			req = setBit(req, 2)
		}
	}

	if _, err := sendMboxCmd(cpu, CONFIG_CLOS, CLOS_PM_QOS_CONFIG, param, req); err != nil {
		return fmt.Errorf("failed to set SST-CP status: %v", err)
	}

	return nil
}

// enableCPMbox enables SST-CP via the Mbox interface and returns the
// updated PM config word.
func enableCPMbox(info *SstPackageInfo, cpu uint16) (uint32, error) {
	if err := writeClosPmQosConfig(info, cpu, true); err != nil {
		return 0, fmt.Errorf("cannot set Clos status: %v", err)
	}

	return writePMConfig(cpu, true)
}

// disableCPMbox disables SST-CP via the Mbox interface and returns the
// updated PM config word.
func disableCPMbox(info *SstPackageInfo, cpu uint16) (uint32, error) {
	if err := writeClosPmQosConfig(info, cpu, false); err != nil {
		return 0, fmt.Errorf("cannot set Clos status: %v", err)
	}

	return writePMConfig(cpu, false)
}

// setDefaultClosParamMbox resets all CLOSes to default values via the Mbox interface.
func setDefaultClosParamMbox(cpu uint16) error {
	defaultConfig := &SstClosInfo{MaxFreq: 255}

	for clos := 0; clos < NumClos; clos++ {
		if err := saveClosMbox(defaultConfig, cpu, uint8(clos)); err != nil {
			return err
		}
	}

	return nil
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

func isBitSet(val, n uint32) bool {
	return val&(1<<n) != 0
}

func setBit(val, n uint32) uint32 {
	return val | (1 << n)
}

func clearBit(val, n uint32) uint32 {
	return val &^ (1 << n)
}
