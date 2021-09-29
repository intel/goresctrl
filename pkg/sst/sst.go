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

	ClosInfo [NumClos]SstClosInfo
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

const isstDevPath = "/dev/isst_interface"

var sstlog grclog.Logger = grclog.NewLoggerWrapper(stdlog.New(os.Stderr, "[ sst ] ", 0))

// SstSupported returns true if Intel Speed Select Technologies (SST) is supported
// by the system and can be interfaced via the Linux kernel device
func SstSupported() bool {
	if _, err := os.Stat(isstDevPath); err != nil {
		if !os.IsNotExist(err) {
			sstlog.Warnf("failed to access sst device %q: %v", isstDevPath, err)
		} else {
			sstlog.Debugf("sst device %q does not exist", isstDevPath)
		}
		return false
	}
	return true
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
	if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_GET_LEVELS_INFO, 0); err != nil {
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

	if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_GET_TDP_CONTROL, uint32(info.PPCurrentLevel)); err != nil {
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
			if rsp, err = sendMboxCmd(cpu, CONFIG_TDP, CONFIG_TDP_PBF_GET_CORE_MASK_INFO, uint32(info.PPCurrentLevel+(i<<8))); err != nil {
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
	if rsp, err = sendMboxCmd(cpu, READ_PM_CONFIG, PM_FEATURE, 0); err != nil {
		return info, fmt.Errorf("failed to read SST CP info: %v", err)
	}

	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)

	if info.CPSupported {
		if rsp, err = sendMboxCmd(cpu, CONFIG_CLOS, CLOS_PM_QOS_CONFIG, 0); err != nil {
			return info, fmt.Errorf("failed to read SST CP status: %v", err)
		}

		closEnabled := isBitSet(rsp, 1)
		if closEnabled != info.CPEnabled {
			sstlog.Warnf("SST firmware returned conflicting CP enabled status %v vs. %v", info.CPEnabled, closEnabled)
		}
		info.CPEnabled = closEnabled
		info.CPPriority = CPPriorityType(getBits(rsp, 2, 2))

		for i := uint32(0); i < NumClos; i++ {
			if rsp, err = sendMMIOCmd(cpu, (i<<2)+PM_CLOS_OFFSET); err != nil {
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
	}

	return info, nil
}

// GetCPUClosID returns the SST-CP CLOS id that a cpu is associated with.
func GetCPUClosID(cpu utils.ID) (int, error) {
	p, err := punitCPU(cpu)
	if err != nil {
		return -1, err
	}
	punitCore := uint32(p) >> 1

	rsp, err := sendMMIOCmd(cpu, (punitCore<<2)+PQR_ASSOC_OFFSET)
	if err != nil {
		return 0, fmt.Errorf("failed to read CLOS number of cpu %d: %v", cpu, err)
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
	rsp, err := sendMboxCmd(info.pkg.cpus[0], CONFIG_TDP, CONFIG_TDP_GET_TDP_CONTROL, uint32(info.PPCurrentLevel))
	if err != nil {
		return fmt.Errorf("failed to read SST status: %w", err)
	}

	req := clearBit(rsp, 17)
	if status {
		req = setBit(rsp, 17)
	}

	if _, err = sendMboxCmd(info.pkg.cpus[0], CONFIG_TDP, CONFIG_TDP_SET_TDP_CONTROL, req); err != nil {
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
	info, err := GetPackageInfo(pkgs...)
	if err != nil {
		return err
	}

	for _, i := range info {
		err = enableBF(i)
		if err != nil {
			// Ignore but log error as there might be packages in the
			// user supplied list that do not exists
			sstlog.Errorf("sst-bf : %w", err)
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
		err = disableBF(i)
		if err != nil {
			// Ignore but log error as there might be packages in the
			// user supplied list that do not exists
			sstlog.Errorf("sst-bf : %w", err)
		}
	}

	return nil
}
