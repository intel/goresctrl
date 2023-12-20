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
	stdlog "log"
	"os"

	grclog "github.com/intel/goresctrl/pkg/log"
	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/utils"
)

// SstPackageInfo contains status of Intel Speed Select Technologies (SST)
// for one CPU package
type SstPackageInfo struct {
	// Package related to this SST info
	pkg *cpuPackageInfo

	// Gereric PP info
	PPSupported    bool
	PPLocked       bool
	PPVersion      int
	PPCurrentLevel int
	PPMaxLevel     int

	// Information about the currently active PP level
	CPSupported bool
	CPEnabled   bool
	CPPriority  CPPriorityType
	BFSupported bool
	BFEnabled   bool
	BFCores     utils.IDSet
	TFSupported bool
	TFEnabled   bool

	ClosInfo    [NumClos]SstClosInfo
	ClosCPUInfo ClosCPUSet
}

// NumClos is the number of CLOSes suported by SST-CP
const NumClos = 4

// SstClosInfo contains parameters of one CLOS of SST-CP
type SstClosInfo struct {
	EPP                  int
	ProportionalPriority int
	MinFreq              int
	MaxFreq              int
	DesiredFreq          int
}

// CPPriorityType denotes the type CLOS priority ordering used in SST-CP
type CPPriorityType int

const (
	Proportional CPPriorityType = 0
	Ordered      CPPriorityType = 1
)

// ClosCPUSet contains mapping from Clos id to a set of CPU ids
type ClosCPUSet map[int]utils.IDSet

var sstlog grclog.Logger = grclog.NewLoggerWrapper(stdlog.New(os.Stderr, "[ sst ] ", 0))

func isstDevPath() string { return goresctrlpath.Path("dev/isst_interface") }

// SstSupported returns true if Intel Speed Select Technologies (SST) is supported
// by the system and can be interfaced via the Linux kernel device
func SstSupported() bool {
	devPath := isstDevPath()
	if _, err := os.Stat(devPath); err != nil {
		if !os.IsNotExist(err) {
			sstlog.Warnf("failed to access sst device %q: %v", devPath, err)
		} else {
			sstlog.Debugf("sst device %q does not exist", devPath)
		}
		return false
	}
	return true
}

// Check that a list of CPUs belong to a given package
func CheckPackageCpus(info *SstPackageInfo, cpus utils.IDSet) bool {
	if info == nil {
		return false
	}

	return info.pkg.hasCpus(cpus)
}

// GetPackageInfo returns information of those packages given as a parameter
// or all if none given.
func GetPackageInfo(pkgs ...int) (map[int]*SstPackageInfo, error) {
	var numPkgs int
	var pkglist []int

	// Get topology information from sysfs
	packages, err := getOnlineCpuPackages()
	if err != nil {
		return nil, fmt.Errorf("failed to determine cpu topology: %w", err)
	}

	if len(pkgs) == 0 {
		for i := range packages {
			pkglist = append(pkglist, i)
		}
	} else {
		for _, i := range pkgs {
			if _, ok := packages[i]; !ok {
				return nil, fmt.Errorf("cpu package %d not present", i)
			} else {
				pkglist = append(pkglist, i)
			}
		}
	}

	numPkgs = len(pkglist)
	infomap := make(map[int]*SstPackageInfo, numPkgs)

	for _, i := range pkglist {
		info, err := getSinglePackageInfo(packages[i])
		if err != nil {
			return nil, err
		}

		infomap[i] = &info
	}

	return infomap, nil
}

// getSinglePackageInfo returns information of the SST configuration of one cpu
// package.
func getSinglePackageInfo(pkg *cpuPackageInfo) (SstPackageInfo, error) {
	info := SstPackageInfo{}

	cpu := pkg.cpus[0] // We just need to pass one logical cpu from the pkg as an arg

	var rsp uint32
	var err error

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
		sstlog.Infof("SST PP version %d (less than 3), giving up...")
		return info, nil
	}

	// Read the status of currently active perf-profile
	if !info.PPSupported {
		sstlog.Debugf("SST PP feature not supported, only profile level %d is valid", info.PPCurrentLevel)
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

		punitCoreIDs := make(map[utils.ID]utils.IDSet, len(pkg.cpus))
		var maxPunitCore utils.ID
		for _, id := range pkg.cpus {
			pc, err := punitCPU(id)
			if err != nil {
				return info, err
			}
			punitCore := pc >> 1
			if _, ok := punitCoreIDs[punitCore]; !ok {
				punitCoreIDs[punitCore] = utils.IDSet{}
			}
			punitCoreIDs[punitCore].Add(id)
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
					info.BFCores.Add(punitCoreIDs[utils.ID(i*32+bit)].Members()...)
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
			closId, err := GetCPUClosID(id)
			if err != nil {
				continue
			}

			if info.ClosCPUInfo[closId] == nil {
				info.ClosCPUInfo[closId] = utils.NewIDSet(id)
			} else {
				info.ClosCPUInfo[closId].Add(id)
			}
		}
	}

	return info, nil
}

func getPunitCoreId(cpu utils.ID) (uint32, error) {
	p, err := punitCPU(cpu)
	if err != nil {
		return 0, err
	}
	punitCore := uint32(p) >> 1

	return punitCore, nil
}

// GetCPUClosID returns the SST-CP CLOS id that a cpu is associated with.
func GetCPUClosID(cpu utils.ID) (int, error) {
	punitCore, err := getPunitCoreId(cpu)
	if err != nil {
		return -1, fmt.Errorf("invalid core id %d for cpu %d: %v", punitCore, cpu, err)
	}

	rsp, err := sendClosCmd(cpu, CLOS_PQR_ASSOC, punitCore, 0)
	if err != nil {
		return -1, fmt.Errorf("failed to read CLOS number of cpu %d: %v", cpu, err)
	}
	return int(getBits(rsp, 16, 17)), nil
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

func setBFStatus(info *SstPackageInfo, status bool) error {
	rsp, err := sendMboxCmd(info.pkg.cpus[0], CONFIG_TDP, CONFIG_TDP_GET_TDP_CONTROL, 0, uint32(info.PPCurrentLevel))
	if err != nil {
		return fmt.Errorf("failed to read SST status: %w", err)
	}

	req := clearBit(rsp, 17)
	if status {
		req = setBit(rsp, 17)
	}

	if _, err = sendMboxCmd(info.pkg.cpus[0], CONFIG_TDP, CONFIG_TDP_SET_TDP_CONTROL, 0, req); err != nil {
		return fmt.Errorf("failed to enable SST %s: %w", "BF", err)
	}

	info.BFEnabled = status

	return nil
}

func setScalingMin2CPUInfoMax(info *SstPackageInfo) error {
	for _, cpu := range info.pkg.cpus {
		err := setCPUScalingMin2CPUInfoMaxFreq(cpu)
		if err != nil {
			return err
		}
	}

	return nil
}

func enableBF(info *SstPackageInfo) error {
	if !info.BFSupported {
		return fmt.Errorf("SST BF not supported")
	}

	if err := setBFStatus(info, true); err != nil {
		return err
	}

	if err := setScalingMin2CPUInfoMax(info); err != nil {
		return err
	}

	return nil
}

// EnableBF enables SST-BF and sets it up properly
func EnableBF(pkgs ...int) error {
	if ok, err := isHWPEnabled(); err != nil {
		return fmt.Errorf("Failed to determine if HWP is enabled")
	} else if !ok {
		return fmt.Errorf("HWP is not enabled")
	}

	info, err := GetPackageInfo(pkgs...)
	if err != nil {
		return err
	}

	for _, i := range info {
		if err := enableBF(i); err != nil {
			return err
		}
	}

	return nil
}

func setScalingMin2CPUInfoMin(info *SstPackageInfo) error {
	for _, cpu := range info.pkg.cpus {
		err := setCPUScalingMin2CPUInfoMinFreq(cpu)
		if err != nil {
			return err
		}
	}

	return nil
}

func disableBF(info *SstPackageInfo) error {
	if !info.BFSupported {
		return fmt.Errorf("SST BF not supported")
	}

	if err := setBFStatus(info, false); err != nil {
		return err
	}

	if err := setScalingMin2CPUInfoMin(info); err != nil {
		return err
	}

	return nil
}

// DisableBF disables SST-BF and clears things properly
func DisableBF(pkgs ...int) error {
	info, err := GetPackageInfo(pkgs...)
	if err != nil {
		return err
	}

	for _, i := range info {
		if err := disableBF(i); err != nil {
			return err
		}
	}

	return nil
}

func sendClosCmd(cpu utils.ID, subCmd uint16, parameter uint32, reqData uint32) (uint32, error) {
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

func saveClos(closInfo *SstClosInfo, cpu utils.ID, clos int) error {
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

func associate2Clos(cpu utils.ID, clos int) error {
	coreId, err := getPunitCoreId(cpu)
	if err != nil {
		return fmt.Errorf("invalid core id %d for cpu %d: %v", coreId, cpu, err)
	}

	req := (clos & 0x03) << 16
	param := setBit(coreId, MBOX_CMD_WRITE_BIT)

	if _, err := sendClosCmd(cpu, CLOS_PQR_ASSOC, param, uint32(req)); err != nil {
		return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
	}

	return nil
}

func writePMConfig(info *SstPackageInfo, cpu utils.ID, enable bool) (uint32, error) {
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

func writeClosPmQosConfig(info *SstPackageInfo, cpu utils.ID, enable bool) error {
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

func enableCP(info *SstPackageInfo, cpu utils.ID) (uint32, error) {
	if err := writeClosPmQosConfig(info, cpu, true); err != nil {
		return 0, fmt.Errorf("Cannot set Clos status: %v", err)
	}

	return writePMConfig(info, cpu, true)
}

func disableCP(info *SstPackageInfo, cpu utils.ID) (uint32, error) {
	if err := writeClosPmQosConfig(info, cpu, false); err != nil {
		return 0, fmt.Errorf("Cannot set Clos status: %v", err)
	}

	return writePMConfig(info, cpu, false)
}

func setDefaultClosParam(info *SstPackageInfo, cpu utils.ID) error {
	defaultConfig := &SstClosInfo{MaxFreq: 255}

	for clos := 0; clos < 4; clos++ {
		if err := saveClos(defaultConfig, cpu, clos); err != nil {
			return err
		}
	}

	return nil
}

func assignCPU2Clos(info *SstPackageInfo, clos int) error {
	sstlog.Debugf("Setting Clos %d for cpus %v\n", clos, info.ClosCPUInfo[clos].Members())

	for _, cpu := range info.ClosCPUInfo[clos].Members() {
		if err := associate2Clos(cpu, clos); err != nil {
			return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
		}
	}

	return nil
}

// ConfigureCP will allow caller to configure CPUs to various Clos.
func ConfigureCP(info *SstPackageInfo, priority int, cpu2clos *ClosCPUSet) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}

	if priority < 0 || priority > 1 {
		return fmt.Errorf("Invalid CP priority value %d (valid 0 or 1)", priority)
	}

	if info.ClosCPUInfo == nil {
		info.ClosCPUInfo = make(map[int]utils.IDSet, len(*cpu2clos))
	}

	for clos, cpus := range *cpu2clos {
		info.ClosCPUInfo[clos] = cpus.Clone()

		// Remove the CPU from other Clos if found
		for i := 0; i < NumClos; i++ {
			if i == clos {
				continue
			}

			for id := range cpus {
				info.ClosCPUInfo[i].Del(id)
			}
		}

		if err := assignCPU2Clos(info, clos); err != nil {
			return err
		}
	}

	info.CPPriority = CPPriorityType(priority)

	return nil
}

// ClosSetup stores the user supplied Clos information into punit
func ClosSetup(info *SstPackageInfo, clos int, closInfo *SstClosInfo) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}

	if clos < 0 || clos >= NumClos {
		return fmt.Errorf("Invalid Clos value (%d)", clos)
	}

	if closInfo.MinFreq < 0 || closInfo.MinFreq > 255 {
		return fmt.Errorf("Invalid min freq (%d)", closInfo.MinFreq)
	}

	if closInfo.MaxFreq < 0 || closInfo.MaxFreq > 255 {
		return fmt.Errorf("Invalid max freq (%d)", closInfo.MaxFreq)
	}

	if closInfo.MinFreq > closInfo.MaxFreq {
		return fmt.Errorf("Min freq %d must be smaller than max freq %d", closInfo.MinFreq, closInfo.MaxFreq)
	}

	if closInfo.DesiredFreq < 0 || closInfo.DesiredFreq > 255 {
		return fmt.Errorf("Invalid value %d for desired freq", closInfo.DesiredFreq)
	}

	if closInfo.EPP < 0 || closInfo.EPP > 15 {
		return fmt.Errorf("Invalid value %d for EPP", closInfo.EPP)
	}

	if closInfo.ProportionalPriority < 0 || closInfo.ProportionalPriority > 15 {
		return fmt.Errorf("Invalid value %d for proportionalPriority", closInfo.ProportionalPriority)
	}

	info.ClosInfo[clos] = *closInfo

	return saveClos(&info.ClosInfo[clos], info.pkg.cpus[0], clos)
}

// ResetCPConfig will bring the system to a known state. This means that all
// CLOS groups are reset to their default values, all package cores are assigned to
// CLOS group 0 and ordered priority mode is enabled.
func ResetCPConfig() error {
	infomap, err := GetPackageInfo()
	if err != nil {
		return err
	}

	for _, info := range infomap {
		for _, cpu := range info.pkg.cpus {
			if info.pkg.cpus[0] == cpu {
				if err := setDefaultClosParam(info, cpu); err != nil {
					return err
				}
			}

			if err := associate2Clos(cpu, 0); err != nil {
				return fmt.Errorf("failed to associate cpu %d to clos %d: %w", cpu, 0, err)
			}
		}
	}

	return nil
}

// EnableCP enables SST-CP feature
func EnableCP(info *SstPackageInfo) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}
	if !info.CPSupported {
		return fmt.Errorf("SST CP not supported")
	}

	if len(info.ClosCPUInfo) == 0 {
		return fmt.Errorf("failed to enable CP: Clos to CPU mapping missing")
	}

	rsp, err := enableCP(info, info.pkg.cpus[0])
	if err != nil {
		return fmt.Errorf("failed to enable SST-CP: %v", err)
	}

	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)

	return nil
}

// DisableCP disables SST-CP feature
func DisableCP(info *SstPackageInfo) error {
	if !info.CPSupported {
		return fmt.Errorf("SST CP not supported")
	}

	if info.TFEnabled {
		return fmt.Errorf("SST TF still enabled, disable it first.")
	}

	rsp, err := disableCP(info, info.pkg.cpus[0])
	if err != nil {
		return fmt.Errorf("failed to disable SST-CP: %v", err)
	}

	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)

	return nil
}
