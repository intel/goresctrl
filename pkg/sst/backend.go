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
	"math"
	"math/bits"
	"sync"

	"github.com/intel/goresctrl/pkg/sst/isst"
	"github.com/intel/goresctrl/pkg/sst/mbox"
	"github.com/intel/goresctrl/pkg/sst/tpmi"
	"github.com/intel/goresctrl/pkg/utils"
)

// numClos is the number of CLOSes supported.
const numClos = 4

// backend handles hardware dispatch, providing a unified interface for both TPMI and Mbox interfaces.
type backend struct {
	apiVersion     int
	apiVersionOnce sync.Once
}

func (h *backend) platformAPIVersion() int {
	h.apiVersionOnce.Do(func() {
		v, err := isst.GetPlatformAPIVersion()
		if err != nil {
			sstlog.Debug("failed to get ISST platform API version, assuming 1 (mbox)", "error", err)
			v = 1
		}
		h.apiVersion = v
	})
	return h.apiVersion
}

func (h *backend) isTPMIPlatform() bool {
	return h.platformAPIVersion() >= 2
}

// resolvePunits populates pkg.punits with punit topology.
func (h *backend) resolvePunits(pkg *cpuPackageInfo) error {
	if h.isTPMIPlatform() {
		punitIDs, err := tpmi.GetSocketPunits(pkg.id)
		if err != nil {
			return err
		}
		puMap := make(map[uint8]*punitInfo, len(punitIDs))
		for _, id := range punitIDs {
			puMap[id] = &punitInfo{
				id:             id,
				cpus:           make(utils.IDSet),
				cpuPunitCoreID: make(map[utils.ID]utils.ID),
				coreCPUs:       make(map[utils.ID]utils.IDSet),
			}
		}
		for _, cpu := range pkg.cpus.Members() {
			punitID, err := tpmi.GetCPUPunitID(uint16(cpu))
			if err != nil {
				return fmt.Errorf("failed to get punit for cpu %d: %w", cpu, err)
			}
			if pu, ok := puMap[punitID]; ok {
				pu.cpus.Add(cpu)
				punitCoreID, err := tpmi.GetPunitCoreID(uint16(cpu))
				if err != nil {
					return fmt.Errorf("failed to get punit core for cpu %d: %w", cpu, err)
				}
				coreID := utils.ID(punitCoreID)
				pu.cpuPunitCoreID[cpu] = coreID
				if pu.coreCPUs[coreID] == nil {
					pu.coreCPUs[coreID] = make(utils.IDSet)
				}
				pu.coreCPUs[coreID].Add(cpu)
			} else {
				sstlog.Warn("CPU has punit ID not in Valid_mask, skipping", "cpu", cpu, "punitID", punitID, "validPunits", punitIDs)
			}
		}
		pkg.punits = make([]*punitInfo, 0, len(punitIDs))
		for _, id := range punitIDs {
			pkg.punits = append(pkg.punits, puMap[id])
		}
		return nil
	}

	// Mbox: single package-wide power domain.
	pu := &punitInfo{
		id:       0,
		cpus:     pkg.cpus,
		coreCPUs: make(map[utils.ID]utils.IDSet),
	}
	for _, cpu := range pkg.cpus.Members() {
		pc, err := isst.GetPunitCPUId(uint16(cpu))
		if err != nil {
			return fmt.Errorf("failed to get punit cpu id for cpu %d: %w", cpu, err)
		}
		coreID := utils.ID((pc & 0x1ff) >> 1)
		if pu.coreCPUs[coreID] == nil {
			pu.coreCPUs[coreID] = make(utils.IDSet)
		}
		pu.coreCPUs[coreID].Add(cpu)
	}
	pkg.punits = []*punitInfo{pu}
	return nil
}

// ppLevelsFromMask decodes a TPMI Level_mask bitmask into the list of available PP levels (bit N set = level N available).
func ppLevelsFromMask(mask uint8) []int {
	var levels []int
	for i := range 8 {
		if mask&(1<<uint(i)) != 0 {
			levels = append(levels, i)
		}
	}
	return levels
}

// ppLevelsFromMax returns a contiguous list of PP levels [0..maxLevel] as used by the Mbox interface.
func ppLevelsFromMax(maxLevel int) []int {
	levels := make([]int, maxLevel+1)
	for i := range levels {
		levels[i] = i
	}
	return levels
}

// puCPUMaskToIDSet converts a 64-bit punit core bitmask to a set of logical CPU IDs
// using the punit's coreCPUs reverse map.
func puCPUMaskToIDSet(pu *punitInfo, mask uint64) utils.IDSet {
	cpus := make(utils.IDSet)
	for m := mask; m != 0; m &= m - 1 {
		bit := utils.ID(bits.TrailingZeros64(m))
		if set, ok := pu.coreCPUs[bit]; ok {
			cpus.Add(set.Members()...)
		}
	}
	return cpus
}

func (h *backend) getPerfLevelInfo(pkg *cpuPackageInfo, level int) (map[utils.ID]*PerfLevelInfo, error) {
	if h.isTPMIPlatform() {
		return getPerfLevelInfoTPMI(pkg, level)
	}
	return getPerfLevelInfoMbox(pkg, level)
}

func (h *backend) getPackageStatus(pkg *cpuPackageInfo) (*PackageStatus, error) {
	if h.isTPMIPlatform() {
		return getPackageInfoTPMI(pkg)
	}
	return getPackageInfoMbox(pkg)
}

func (h *backend) cpSetStatus(pkg *cpuPackageInfo, pu *punitInfo, enable bool) error {
	if h.isTPMIPlatform() {
		priority, err := tpmi.CPGetPriorityType(pkg.id, pu.id)
		if err != nil {
			return err
		}
		return tpmi.CPSetState(pkg.id, pu.id, enable, priority)
	}

	priority, err := mbox.CPGetPriorityType(uint16(pu.cpus.Members()[0]))
	if err != nil {
		return err
	}
	return mbox.CPSetStatus(uint16(pu.cpus.Members()[0]), enable, priority)
}

func (h *backend) cpSetPriorityType(pkg *cpuPackageInfo, pu *punitInfo, priority CPPriorityType) error {
	if h.isTPMIPlatform() {
		return tpmi.CPSetPriorityType(pkg.id, pu.id, uint8(priority))
	}
	return mbox.CPSetPriorityType(uint16(pu.cpus.Members()[0]), int(priority))
}

func (h *backend) closCount() int {
	// Currently hardcoded to 4 for both TPMI and Mbox, but could be queried from hardware in the future.
	return numClos
}

func (h *backend) closAssociate(pkg *cpuPackageInfo, cpu utils.ID, clos int) error {
	if cpu < 0 || cpu > math.MaxUint16 {
		return fmt.Errorf("CPU id %d out of range", cpu)
	}
	if clos < 0 || clos >= numClos {
		return fmt.Errorf("CLOS id %d out of range (valid: 0-%d)", clos, numClos-1)
	}
	cpuID, closID := uint16(cpu), uint8(clos)
	if h.isTPMIPlatform() {
		pu := pkg.punitForCPU(cpuID)
		if pu == nil {
			return fmt.Errorf("cpu %d not found in package %d", cpu, pkg.id)
		}
		return tpmi.ClosAssociate(pkg.id, pu.id, uint16(pu.cpuPunitCoreID[utils.ID(cpuID)]), closID)
	}
	return mbox.ClosAssociate(cpuID, closID)
}

func (h *backend) getCPUClosID(cpu utils.ID) (int, error) {
	if cpu < 0 || cpu > math.MaxUint16 {
		return -1, fmt.Errorf("CPU id %d out of range", cpu)
	}
	cpuID := uint16(cpu)
	if h.isTPMIPlatform() {
		ci, err := tpmiGetCPUTopo(cpuID)
		if err != nil {
			return -1, err
		}
		closID, err := tpmi.GetCPUClosID(ci.socketID, ci.punitID, ci.punitCoreID)
		return int(closID), err
	}
	closID, err := mbox.GetCPUClosID(cpuID)
	return int(closID), err
}

func (h *backend) closConfigure(pkg *cpuPackageInfo, pu *punitInfo, clos int, config *ClosConfig) error {
	if clos < 0 || clos >= numClos {
		return fmt.Errorf("CLOS id %d out of range (valid: 0-%d)", clos, numClos-1)
	}
	if config.MinFreq > config.MaxFreq {
		return fmt.Errorf("min freq %d MHz must be smaller than max freq %d MHz", config.MinFreq, config.MaxFreq)
	}

	closID := uint8(clos)
	if h.isTPMIPlatform() {
		if config.ProportionalPriority < 0 || config.ProportionalPriority > math.MaxUint8 {
			return fmt.Errorf("invalid value %d for proportionalPriority", config.ProportionalPriority)
		}
		if config.MinFreq < 0 || config.MinFreq > math.MaxUint16 {
			return fmt.Errorf("invalid min freq (%d MHz)", config.MinFreq)
		}
		if config.MaxFreq < 0 || config.MaxFreq > math.MaxUint16 {
			return fmt.Errorf("invalid max freq (%d MHz)", config.MaxFreq)
		}
		if config.epp != 0 {
			sstlog.Warn("ignoring EPP value, not supported on this platform", "clos", clos, "punit", pu.id, "epp", config.epp)
		}
		if config.desiredFreq != 0 {
			sstlog.Warn("ignoring DesiredFreq value, not supported on this platform", "clos", clos, "punit", pu.id, "desiredFreq", config.desiredFreq)
		}
		return tpmi.ClosSetParam(pkg.id, pu.id, closID, uint8(config.ProportionalPriority), uint16(config.MinFreq), uint16(config.MaxFreq))
	}

	// Mbox: Proportional priority is a 4-bit field
	if config.ProportionalPriority < 0 || config.ProportionalPriority > 15 {
		return fmt.Errorf("invalid value %d for proportionalPriority", config.ProportionalPriority)
	}
	// Mbox: frequencies are stored as 8-bit ratios (MHz ÷ 100), so 0–25500 MHz
	if config.MinFreq < 0 || config.MinFreq > 25500 {
		return fmt.Errorf("invalid min freq (%d MHz)", config.MinFreq)
	}
	if config.MaxFreq < 0 || config.MaxFreq > 25500 {
		return fmt.Errorf("invalid max freq (%d MHz)", config.MaxFreq)
	}
	if config.desiredFreq < 0 || config.desiredFreq > 25500 {
		return fmt.Errorf("invalid value %d for desired freq", config.desiredFreq)
	}
	if config.epp < 0 || config.epp > 15 {
		return fmt.Errorf("invalid value %d for EPP", config.epp)
	}
	return mbox.ClosSetParam(uint16(pu.cpus.Members()[0]), closID, uint8(config.epp), uint8(config.ProportionalPriority), uint8(config.MinFreq/100), uint8(config.MaxFreq/100), uint8(config.desiredFreq/100))
}

func (h *backend) closResetConfiguration(pkg *cpuPackageInfo, pu *punitInfo) error {
	if h.isTPMIPlatform() {
		return tpmi.ClosReset(pkg.id, pu.id)
	}
	return mbox.ClosReset(uint16(pu.cpus.Members()[0]))
}

func (h *backend) bfSetStatus(pkg *cpuPackageInfo, pu *punitInfo, enable bool) error {
	if enable {
		if ok, err := isHWPEnabled(); err != nil {
			return fmt.Errorf("failed to determine if HWP is enabled: %w", err)
		} else if !ok {
			return fmt.Errorf("HWP is not enabled")
		}
	}

	if h.isTPMIPlatform() {
		return tpmi.BFSetStatus(pkg.id, pu.id, enable)
	}
	cpu := uint16(pu.cpus.Members()[0])
	ppCurrentLevel, err := mbox.PPGetCurrentLevel(cpu)
	if err != nil {
		return err
	}
	return mbox.BFSetStatus(cpu, ppCurrentLevel, enable)
}

func (h *backend) tfSetStatus(pkg *cpuPackageInfo, pu *punitInfo, enable bool) error {
	if h.isTPMIPlatform() {
		return tpmi.TFSetStatus(pkg.id, pu.id, enable)
	}
	cpu := uint16(pu.cpus.Members()[0])
	ppCurrentLevel, err := mbox.PPGetCurrentLevel(cpu)
	if err != nil {
		return err
	}
	return mbox.TFSetStatus(cpu, ppCurrentLevel, enable)
}

// tpmiCPUTopo holds the resolved TPMI identifiers for a logical CPU.
type tpmiCPUTopo struct {
	socketID    uint8
	punitID     uint8
	punitCoreID uint16
}

// tpmiGetCPUTopo resolves the socket, punit, and punit core identifiers for a logical CPU.
func tpmiGetCPUTopo(cpu uint16) (tpmiCPUTopo, error) {
	socketID, err := getCPUSocketID(cpu)
	if err != nil {
		return tpmiCPUTopo{}, fmt.Errorf("failed to get socket for cpu %d: %w", cpu, err)
	}
	punitID, err := tpmi.GetCPUPunitID(cpu)
	if err != nil {
		return tpmiCPUTopo{}, fmt.Errorf("failed to get punit for cpu %d: %w", cpu, err)
	}
	punitCoreID, err := tpmi.GetPunitCoreID(cpu)
	if err != nil {
		return tpmiCPUTopo{}, fmt.Errorf("failed to get punit core for cpu %d: %w", cpu, err)
	}
	return tpmiCPUTopo{socketID: socketID, punitID: punitID, punitCoreID: punitCoreID}, nil
}

// getPackageInfoTPMI reads SST information for one CPU package via the TPMI interface.
func getPackageInfoTPMI(pkg *cpuPackageInfo) (*PackageStatus, error) {
	socketID := pkg.id

	info := &PackageStatus{
		ID:     utils.ID(socketID),
		Punits: make(map[utils.ID]*PunitStatus),
	}

	for _, pu := range pkg.punits {
		punitID := pu.id

		perfInfo, err := tpmi.PPGetPerfLevels(socketID, punitID)
		if err != nil {
			return nil, err
		}

		punit := &PunitStatus{
			CPUs: pu.cpus.Clone(),
			PP: PPStatus{
				Supported:    perfInfo.Enabled != 0,
				Locked:       perfInfo.Locked != 0,
				Version:      int(perfInfo.Feature_rev),
				CurrentLevel: int(perfInfo.Current_level),
				MaxLevel:     int(perfInfo.Max_level),
				Levels:       ppLevelsFromMask(perfInfo.Level_mask),
			},
			BF: BFStatus{
				Supported: perfInfo.Sst_bf_support != 0,
				Enabled:   (perfInfo.Feature_state & 0x01) != 0,
			},
			TF: TFStatus{
				Supported: perfInfo.Sst_tf_support != 0,
				Enabled:   (perfInfo.Feature_state & 0x02) != 0,
			},
		}

		// Read CP state for this punit
		cpState, err := tpmi.CPGetState(socketID, punitID)
		if err != nil {
			return nil, err
		}
		punit.CP.Supported = cpState.Supported != 0
		punit.CP.Enabled = cpState.Enable != 0
		punit.CP.Priority = CPPriorityType(cpState.Priority_type)

		if punit.CP.Supported {
			punit.Clos = make([]ClosStatus, numClos)
			for i := 0; i < numClos; i++ {
				closParam, err := tpmi.ClosGetParam(socketID, punitID, uint8(i))
				if err != nil {
					return nil, err
				}
				punit.Clos[i] = ClosStatus{
					Config: ClosConfig{
						ProportionalPriority: int(closParam.Prop_prio),
						MinFreq:              int(closParam.Min_freq_mhz),
						MaxFreq:              int(closParam.Max_freq_mhz),
					},
				}
			}
			for _, id := range pu.cpus.Members() {
				closID, err := tpmi.GetCPUClosID(socketID, punitID, uint16(pu.cpuPunitCoreID[id]))
				if err != nil {
					sstlog.Debug("failed to get CLOS id for cpu", "cpu", id, "error", err)
					continue
				}
				if punit.Clos[closID].CPUs == nil {
					punit.Clos[closID].CPUs = utils.NewIDSet(id)
				} else {
					punit.Clos[closID].CPUs.Add(id)
				}
			}
		}

		info.Punits[utils.ID(punitID)] = punit
	}

	return info, nil
}

// getPackageInfoMbox reads SST information for one CPU package via the Mbox interface.
func getPackageInfoMbox(pkg *cpuPackageInfo) (*PackageStatus, error) {
	cpu := uint16(pkg.cpus.Members()[0])

	ppInfo, err := mbox.PPReadInfo(cpu)
	if err != nil {
		return nil, err
	}

	info := &PackageStatus{
		ID:     utils.ID(pkg.id),
		Punits: make(map[utils.ID]*PunitStatus),
	}

	if ppInfo.Version < 3 {
		sstlog.Info("SST PP version less than 3, giving up...", "version", ppInfo.Version)
		punit := &PunitStatus{
			CPUs: make(utils.IDSet),
			PP: PPStatus{
				Supported:    ppInfo.Supported,
				Locked:       ppInfo.Locked,
				Version:      ppInfo.Version,
				CurrentLevel: ppInfo.CurrentLevel,
				MaxLevel:     ppInfo.MaxLevel,
				Levels:       ppLevelsFromMax(ppInfo.MaxLevel),
			},
		}
		for _, id := range pkg.cpus.Members() {
			punit.CPUs.Add(id)
		}
		info.Punits[0] = punit
		return info, nil
	}

	control, err := mbox.PPReadTDPControl(cpu, ppInfo.CurrentLevel)
	if err != nil {
		return nil, err
	}

	cpStatus, err := mbox.CPReadStatus(cpu)
	if err != nil {
		return nil, err
	}

	var cpPriority CPPriorityType
	closInfos := make([]ClosStatus, numClos)

	if cpStatus.Supported {
		priorityType, err := mbox.CPGetPriorityType(cpu)
		if err != nil {
			return nil, err
		}
		cpPriority = CPPriorityType(priorityType)

		for i := 0; i < numClos; i++ {
			cp, err := mbox.ClosGetParam(cpu, i)
			if err != nil {
				return nil, err
			}
			closInfos[i] = ClosStatus{
				Config: ClosConfig{
					ProportionalPriority: cp.Proportional,
					MinFreq:              cp.MinFreq * 100,
					MaxFreq:              cp.MaxFreq * 100,
					epp:                  cp.Epp,
					desiredFreq:          cp.DesiredFreq * 100,
				},
			}
		}

		for _, id := range pkg.cpus.Members() {
			closID, err := mbox.GetCPUClosID(uint16(id))
			if err != nil {
				continue
			}
			if closInfos[int(closID)].CPUs == nil {
				closInfos[int(closID)].CPUs = utils.NewIDSet(id)
			} else {
				closInfos[int(closID)].CPUs.Add(id)
			}
		}
	}

	punit := &PunitStatus{
		CPUs: pkg.cpus.Clone(),
		PP: PPStatus{
			Supported:    ppInfo.Supported,
			Locked:       ppInfo.Locked,
			Version:      ppInfo.Version,
			CurrentLevel: ppInfo.CurrentLevel,
			MaxLevel:     ppInfo.MaxLevel,
			Levels:       ppLevelsFromMax(ppInfo.MaxLevel),
		},
		BF: BFStatus{
			Supported: control.BFSupported,
			Enabled:   control.BFEnabled,
		},
		TF: TFStatus{
			Supported: control.TFSupported,
			Enabled:   control.TFEnabled,
		},
		CP: CPStatus{
			Supported: cpStatus.Supported,
			Enabled:   cpStatus.Enabled,
			Priority:  cpPriority,
		},
		Clos: closInfos,
	}
	info.Punits[0] = punit

	return info, nil
}

// getPerfLevelInfoTPMI retrieves detailed PP level info for each TPMI punit.
func getPerfLevelInfoTPMI(pkg *cpuPackageInfo, level int) (map[utils.ID]*PerfLevelInfo, error) {
	result := make(map[utils.ID]*PerfLevelInfo, len(pkg.punits))
	socketID := pkg.id

	for _, pu := range pkg.punits {
		punitID := pu.id

		// Validate requested level against this punit's level mask.
		perfInfo, err := tpmi.PPGetPerfLevels(socketID, punitID)
		if err != nil {
			return nil, fmt.Errorf("punit %d: failed to read perf levels: %w", punitID, err)
		}
		if perfInfo.Level_mask&(1<<uint(level)) == 0 {
			return nil, fmt.Errorf("punit %d: level %d not available (mask %#02x)", punitID, level, perfInfo.Level_mask)
		}

		cpuMask, err := tpmi.PerfLevelGetCPUMask(socketID, punitID, uint8(level))
		if err != nil {
			return nil, fmt.Errorf("punit %d: %w", punitID, err)
		}

		info := &PerfLevelInfo{
			CPUs: puCPUMaskToIDSet(pu, cpuMask),
		}

		// BF info
		if perfInfo.Sst_bf_support != 0 {
			bfData, err := tpmi.BFGetInfo(socketID, punitID, uint8(level))
			if err != nil {
				return nil, fmt.Errorf("punit %d: BF info: %w", punitID, err)
			}
			bfMask, err := tpmi.BFGetCoreMask(socketID, punitID, uint8(level))
			if err != nil {
				return nil, fmt.Errorf("punit %d: BF core mask: %w", punitID, err)
			}
			info.BF = BFInfo{
				Supported:            true,
				HighPriorityBaseFreq: int(bfData.High_base_freq_mhz),
				LowPriorityBaseFreq:  int(bfData.Low_base_freq_mhz),
				HighPriorityCPUs:     puCPUMaskToIDSet(pu, bfMask),
			}
		}

		// TF info
		if perfInfo.Sst_tf_support != 0 {
			tfData, err := tpmi.TFGetInfo(socketID, punitID, uint8(level))
			if err != nil {
				return nil, fmt.Errorf("punit %d: TF info: %w", punitID, err)
			}
			numClip := min(int(tfData.Max_clip_freqs), len(tfData.Lp_clip_freq_mhz))
			numTFLevels := min(int(tfData.Max_trl_levels), len(tfData.Trl_freq_mhz))
			numTFBuckets := min(int(tfData.Max_buckets), len(tfData.Bucket_core_counts))
			var lpClip []TRLFreqInfo
			for i := range numClip {
				if f := int(tfData.Lp_clip_freq_mhz[i]); f != 0 {
					lpClip = append(lpClip, TRLFreqInfo{ID: i, Freq: f})
				}
			}
			var tfBuckets []TFBucketInfo
			for b := range numTFBuckets {
				coreCount := int(tfData.Bucket_core_counts[b])
				if coreCount == 0 {
					continue
				}
				var freqs []TRLFreqInfo
				for l := range numTFLevels {
					if f := int(tfData.Trl_freq_mhz[l][b]); f != 0 {
						freqs = append(freqs, TRLFreqInfo{ID: l, Freq: f})
					}
				}
				tfBuckets = append(tfBuckets, TFBucketInfo{
					ID:                    b,
					HighPriorityCoreCount: coreCount,
					MaxFreqs:              freqs,
				})
			}
			info.TF = TFInfo{
				Supported:   true,
				LPClipFreqs: lpClip,
				Buckets:     tfBuckets,
			}
		}

		result[utils.ID(punitID)] = info
	}
	return result, nil
}

// getPerfLevelInfoMbox retrieves detailed PP level info via the Mbox interface.
func getPerfLevelInfoMbox(pkg *cpuPackageInfo, level int) (map[utils.ID]*PerfLevelInfo, error) {
	if len(pkg.punits) == 0 {
		return make(map[utils.ID]*PerfLevelInfo), nil
	}
	pu := pkg.punits[0]
	cpu := uint16(pu.cpus.Members()[0])

	// Validate requested level.
	ppInfo, err := mbox.PPReadInfo(cpu)
	if err != nil {
		return nil, fmt.Errorf("failed to read PP info: %w", err)
	}
	if level < 0 || level > ppInfo.MaxLevel {
		return nil, fmt.Errorf("level %d not available (max %d)", level, ppInfo.MaxLevel)
	}

	cpuMask, err := mbox.PerfLevelGetCoreMask64(cpu, level)
	if err != nil {
		return nil, err
	}

	info := &PerfLevelInfo{
		CPUs: puCPUMaskToIDSet(pu, cpuMask),
	}

	if ppInfo.Supported {
		control, err := mbox.PPReadTDPControl(cpu, level)
		if err != nil {
			return nil, err
		}

		// BF info.
		info.BF.Supported = control.BFSupported
		if control.BFSupported {
			bfData, err := mbox.BFGetInfo(cpu, level)
			if err != nil {
				return nil, fmt.Errorf("BF level data: %w", err)
			}
			info.BF.HighPriorityBaseFreq = bfData.HighPriorityBaseFreqRatio * 100
			info.BF.LowPriorityBaseFreq = bfData.LowPriorityBaseFreqRatio * 100
			info.BF.HighPriorityCPUs = puCPUMaskToIDSet(pu, bfData.CoreMask)
		}

		// TF info.
		info.TF.Supported = control.TFSupported
		if control.TFSupported {
			tfData, err := mbox.TFGetInfo(cpu, level)
			if err != nil {
				return nil, fmt.Errorf("TF level data: %w", err)
			}
			var lpClip []TRLFreqInfo
			for i, r := range tfData.LPClipRatios {
				if f := r * 100; f != 0 {
					lpClip = append(lpClip, TRLFreqInfo{ID: i, Freq: f})
				}
			}
			var tfBuckets []TFBucketInfo
			for bucketID, hpCoreCount := range tfData.HPCoreCounts {
				if hpCoreCount == 0 {
					continue
				}
				var freqs []TRLFreqInfo
				for trlLvl, ratios := range tfData.HPTRLRatios {
					if f := ratios[bucketID] * 100; f != 0 {
						freqs = append(freqs, TRLFreqInfo{ID: trlLvl, Freq: f})
					}
				}
				tfBuckets = append(tfBuckets, TFBucketInfo{
					ID:                    bucketID,
					HighPriorityCoreCount: hpCoreCount,
					MaxFreqs:              freqs,
				})
			}
			info.TF.LPClipFreqs = lpClip
			info.TF.Buckets = tfBuckets
		}
	}

	return map[utils.ID]*PerfLevelInfo{0: info}, nil
}
