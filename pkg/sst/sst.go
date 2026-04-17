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
	"log/slog"
	"math"
	"os"
	"sync"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/utils"
)

// SstPackageInfo contains status of Intel Speed Select Technologies (SST)
// for one CPU package
type SstPackageInfo struct {
	// Package related to this SST info
	pkg *cpuPackageInfo
	// punits for of the package, valid for the TPMI backend only
	tpmiPunits []uint8
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

var sstlog *slog.Logger = slog.Default()

// sstBackend is a dispatcher to select between Mbox (API v1) and TPMI (API v2+) interfaces
type sstBackend struct {
	apiVersion     int
	apiVersionOnce sync.Once
}

// backend is the package-level dispatch object.
var backend = sstBackend{}

// SetLogger sets the logger instance used by the package.
func SetLogger(l *slog.Logger) {
	sstlog = l
}

func isstDevPath() string { return goresctrlpath.Path("dev/isst_interface") }

// SstSupported returns true if Intel Speed Select Technologies (SST) is supported
// by the system and can be interfaced via the Linux kernel device
func SstSupported() bool {
	devPath := isstDevPath()
	if _, err := os.Stat(devPath); err != nil {
		if !os.IsNotExist(err) {
			sstlog.Error("failed to access sst device", "path", devPath, "error", err)
		} else {
			sstlog.Debug("sst device does not exist", "path", devPath)
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
		info, err := backend.getSinglePackageInfo(packages[i])
		if err != nil {
			return nil, err
		}

		infomap[i] = &info
	}

	return infomap, nil
}

// GetCPUClosID returns the SST-CP CLOS id that a cpu is associated with.
func GetCPUClosID(cpu utils.ID) (int, error) {
	return backend.getCPUClosID(cpu)
}

func assignCPU2Clos(info *SstPackageInfo, clos int) error {
	sstlog.Debug("assigning CPUs to SST CLOS", "closID", clos, "cpuset", info.ClosCPUInfo[clos].Members())

	for _, cpu := range info.ClosCPUInfo[clos].Members() {
		if err := backend.associate2Clos(info, utils.ID(cpu), clos); err != nil {
			return fmt.Errorf("failed to associate cpu %d to clos %d: %v", cpu, clos, err)
		}
	}

	return nil
}

func setScalingMin2CPUInfoMax(info *SstPackageInfo) error {
	for _, cpu := range info.pkg.cpus {
		err := setCPUScalingMin2CPUInfoMaxFreq(utils.ID(cpu))
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

	if err := backend.setBFStatus(info, true); err != nil {
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
		return fmt.Errorf("failed to determine if HWP is enabled")
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
		err := setCPUScalingMin2CPUInfoMinFreq(utils.ID(cpu))
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

	if err := backend.setBFStatus(info, false); err != nil {
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

// ConfigureCP will allow caller to configure CPUs to various Clos.
func ConfigureCP(info *SstPackageInfo, priority int, cpu2clos *ClosCPUSet) error {
	if info == nil {
		return fmt.Errorf("package info is nil")
	}

	if priority < 0 || priority > 1 {
		return fmt.Errorf("invalid CP priority value %d (valid 0 or 1)", priority)
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
		return fmt.Errorf("invalid Clos value (%d)", clos)
	}

	if closInfo.MinFreq < 0 || closInfo.MinFreq > 255 {
		return fmt.Errorf("invalid min freq (%d)", closInfo.MinFreq)
	}

	if closInfo.MaxFreq < 0 || closInfo.MaxFreq > 255 {
		return fmt.Errorf("invalid max freq (%d)", closInfo.MaxFreq)
	}

	if closInfo.MinFreq > closInfo.MaxFreq {
		return fmt.Errorf("min freq %d must be smaller than max freq %d", closInfo.MinFreq, closInfo.MaxFreq)
	}

	if closInfo.DesiredFreq < 0 || closInfo.DesiredFreq > 255 {
		return fmt.Errorf("invalid value %d for desired freq", closInfo.DesiredFreq)
	}

	if closInfo.EPP < 0 || closInfo.EPP > 15 {
		return fmt.Errorf("invalid value %d for EPP", closInfo.EPP)
	}

	if closInfo.ProportionalPriority < 0 || closInfo.ProportionalPriority > 15 {
		return fmt.Errorf("invalid value %d for proportionalPriority", closInfo.ProportionalPriority)
	}

	info.ClosInfo[clos] = *closInfo

	return backend.saveClos(info, clos, &info.ClosInfo[clos])
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
		if err := backend.setDefaultClosParam(info); err != nil {
			return err
		}

		for _, cpu := range info.pkg.cpus {
			if err := backend.associate2Clos(info, utils.ID(cpu), 0); err != nil {
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

	if err := backend.enableCP(info); err != nil {
		return fmt.Errorf("failed to enable SST-CP: %v", err)
	}

	return nil
}

// DisableCP disables SST-CP feature
func DisableCP(info *SstPackageInfo) error {
	if !info.CPSupported {
		return fmt.Errorf("SST CP not supported")
	}

	if info.TFEnabled {
		return fmt.Errorf("SST TF still enabled, disable it first")
	}

	if err := backend.disableCP(info); err != nil {
		return fmt.Errorf("failed to disable SST-CP: %v", err)
	}

	return nil
}

// pkgCPU returns the first CPU of the package.
// SstPackageInfo is always created with at least one CPU, so no bounds check is in place.
func (info *SstPackageInfo) pkgCPU() uint16 { return info.pkg.cpus[0] }

func (b *sstBackend) platformAPIVersion() int {
	b.apiVersionOnce.Do(func() {
		v, err := getPlatformAPIVersion()
		if err != nil {
			sstlog.Debug("failed to get ISST platform API version, assuming 1 (mbox)", "error", err)
			v = 1
		}
		b.apiVersion = v
	})
	return b.apiVersion
}

func (b *sstBackend) isTPMIPlatform() bool {
	return b.platformAPIVersion() >= 2
}

func (b *sstBackend) getSinglePackageInfo(pkg *cpuPackageInfo) (SstPackageInfo, error) {
	if b.isTPMIPlatform() {
		return getSinglePackageInfoTPMI(pkg)
	}
	return getSinglePackageInfoMbox(pkg)
}

func (b *sstBackend) getCPUClosID(cpu utils.ID) (int, error) {
	if cpu < 0 || cpu > math.MaxUint16 {
		return -1, fmt.Errorf("CPU id %d out of range", cpu)
	}
	cpuID := uint16(cpu)
	if b.isTPMIPlatform() {
		socketID, err := getCPUSocketID(cpuID)
		if err != nil {
			return -1, err
		}
		punitID, err := getCPUPunitID(cpuID)
		if err != nil {
			return -1, err
		}
		closID, err := getCPUClosIDTPMI(cpuID, socketID, punitID)
		return int(closID), err
	}
	closID, err := getCPUClosIDMbox(cpuID)
	return int(closID), err
}

func (b *sstBackend) setBFStatus(info *SstPackageInfo, enable bool) error {
	if b.isTPMIPlatform() {
		return setBFStatusTPMI(info, enable)
	}
	return setBFStatusMbox(info, enable)
}

func (b *sstBackend) associate2Clos(info *SstPackageInfo, cpu utils.ID, clos int) error {
	if cpu < 0 || cpu > math.MaxUint16 {
		return fmt.Errorf("CPU id %d out of range", cpu)
	}
	if clos < 0 || clos >= NumClos {
		return fmt.Errorf("CLOS id %d out of range (valid: 0-%d)", clos, NumClos-1)
	}
	cpuID, closID := uint16(cpu), uint8(clos)
	if b.isTPMIPlatform() {
		return associate2ClosTPMI(cpuID, info.pkg.id, closID)
	}
	return associate2ClosMbox(cpuID, closID)
}

func (b *sstBackend) saveClos(info *SstPackageInfo, clos int, closInfo *SstClosInfo) error {
	if clos < 0 || clos >= NumClos {
		return fmt.Errorf("CLOS id %d out of range (valid: 0-%d)", clos, NumClos-1)
	}
	closID := uint8(clos)
	if b.isTPMIPlatform() {
		return saveClosTPMI(info, closID, closInfo)
	}
	return saveClosMbox(closInfo, info.pkgCPU(), closID)
}

func (b *sstBackend) setDefaultClosParam(info *SstPackageInfo) error {
	if b.isTPMIPlatform() {
		return setDefaultClosParamTPMI(info)
	}
	return setDefaultClosParamMbox(info.pkgCPU())
}

func (b *sstBackend) enableCP(info *SstPackageInfo) error {
	if b.isTPMIPlatform() {
		return enableCPTPMI(info)
	}
	rsp, err := enableCPMbox(info, info.pkgCPU())
	if err != nil {
		return err
	}
	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)
	return nil
}

func (b *sstBackend) disableCP(info *SstPackageInfo) error {
	if b.isTPMIPlatform() {
		return disableCPTPMI(info)
	}
	rsp, err := disableCPMbox(info, info.pkgCPU())
	if err != nil {
		return err
	}
	info.CPSupported = isBitSet(rsp, 0)
	info.CPEnabled = isBitSet(rsp, 16)
	return nil
}
