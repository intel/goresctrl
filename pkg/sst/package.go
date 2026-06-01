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

	"github.com/intel/goresctrl/pkg/utils"
)

// PackageStatus contains SST information for one package.
type PackageStatus struct {
	ID     utils.ID                  `json:"id"`
	Punits map[utils.ID]*PunitStatus `json:"punits"`
}

// PunitStatus contains SST information for one power domain (punit) within a package.
type PunitStatus struct {
	CPUs utils.IDSet  `json:"cpus"`
	PP   PPStatus     `json:"pp"`
	BF   BFStatus     `json:"bf"`
	TF   TFStatus     `json:"tf"`
	CP   CPStatus     `json:"cp"`
	Clos []ClosStatus `json:"clos,omitempty"`
}

// PPStatus contains SST-PP (Performance Profile) state.
type PPStatus struct {
	Supported    bool  `json:"supported"`
	Locked       bool  `json:"locked"`
	Version      int   `json:"version"`
	CurrentLevel int   `json:"currentLevel"`
	MaxLevel     int   `json:"maxLevel"`
	Levels       []int `json:"levels,omitempty"`
}

// BFStatus contains SST-BF (Base Frequency) state.
type BFStatus struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// TFStatus contains SST-TF (Turbo Frequency) state.
type TFStatus struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// CPStatus contains SST-CP (Core Power) state.
type CPStatus struct {
	Supported bool           `json:"supported"`
	Enabled   bool           `json:"enabled"`
	Priority  CPPriorityType `json:"priority"`
}

// ClosStatus contains the configuration and CPU associations for one CLOS of SST-CP.
type ClosStatus struct {
	Config ClosConfig  `json:"config"`
	CPUs   utils.IDSet `json:"cpus,omitempty"`
}

// ClosConfig contains the configuration parameters of one CLOS of SST-CP.
type ClosConfig struct {
	ProportionalPriority int `json:"proportionalPriority"`
	MinFreq              int `json:"minFreq"`
	MaxFreq              int `json:"maxFreq"`

	// Legacy fields only supported by the old API and Mbox kernel interface
	epp         int
	desiredFreq int
}

// CPPriorityType denotes the type of CLOS priority ordering used in SST-CP.
type CPPriorityType uint8

const (
	Proportional CPPriorityType = 0
	Ordered      CPPriorityType = 1
)

// Validate reports whether p is a recognised priority type.
func (p CPPriorityType) Validate() bool {
	switch p {
	case Proportional, Ordered:
		return true
	default:
		return false
	}
}

// PerfLevelInfo contains detailed SST-PP information for one performance level.
type PerfLevelInfo struct {
	CPUs utils.IDSet `json:"cpus"`
	BF   BFInfo      `json:"bf"`
	TF   TFInfo      `json:"tf"`
}

// BFInfo contains SST-BF (Base Frequency) properties for one performance level.
type BFInfo struct {
	Supported            bool        `json:"supported"`
	HighPriorityBaseFreq int         `json:"highPriorityBaseFreq,omitempty"` // MHz
	LowPriorityBaseFreq  int         `json:"lowPriorityBaseFreq,omitempty"`  // MHz
	HighPriorityCPUs     utils.IDSet `json:"highPriorityCPUs,omitempty"`
}

// TFInfo contains SST-TF (Turbo Frequency) properties for one performance level.
//
// Buckets group CPUs into high-priority sets by core count. Each bucket
// defines how many cores receive elevated turbo frequencies and the maximum
// frequency allowed at each TRL level for the priority cores.
//
// TRL (Turbo Ratio Limit) are limits for certain instruction-set workload
// classes (e.g. SSE, AVX2, AVX-512).
type TFInfo struct {
	Supported bool `json:"supported"`
	// LPClipFreqs contains the low-priority core frequency limits for each TRL level.
	LPClipFreqs []TRLFreqInfo `json:"lpClipFreqs,omitempty"`
	// Buckets contain info about TF buckets.
	Buckets []TFBucketInfo `json:"buckets,omitempty"`
}

// TFBucketInfo holds SST-TF high-priority bucket data for one performance level.
// Empty buckets (core count == 0) are omitted.
type TFBucketInfo struct {
	ID                    int `json:"id"`
	HighPriorityCoreCount int `json:"highPriorityCoreCount"`
	// MaxFreqs contains maximum turbo frequency for each TRL level.
	MaxFreqs []TRLFreqInfo `json:"maxFreqs"`
}

// TRLFreqInfo holds a TRL level ID and its associated frequency in MHz.
type TRLFreqInfo struct {
	ID   int `json:"id"`
	Freq int `json:"freq"` // MHz
}

// Package provides SST operations for one CPU package.
type Package struct {
	h   *Platform
	pkg *cpuPackageInfo
}

// ID returns the package ID.
func (p *Package) ID() utils.ID {
	return int(p.pkg.id)
}

// GetStatus returns the current status of SST features.
func (p *Package) GetStatus() (*PackageStatus, error) {
	return p.h.getPackageStatus(p.pkg)
}

// GetPerfLevelInfo returns detailed SST-PP data for the given performance level, indexed by punit ID.
// Returns an error if the level is not available on any punit.
func (p *Package) GetPerfLevelInfo(level int) (map[utils.ID]*PerfLevelInfo, error) {
	return p.h.getPackagePerfLevelInfo(p.pkg, level)
}

// BFEnable enables SST-BF for this package.
// NOTE: The caller should ensure that the sysfs cpufreq scaling limits of the
// affected CPUs allow higher base frequency on high-priority cores; see
// [utils.GetCPUFreqValue], [utils.SetCPUScalingMinFreq], and
// [utils.SetCPUScalingMaxFreq].
func (p *Package) BFEnable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.bfSetStatus(p.pkg, pu, true); err != nil {
			return err
		}
	}
	return nil
}

// BFDisable disables SST-BF for this package.
func (p *Package) BFDisable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.bfSetStatus(p.pkg, pu, false); err != nil {
			return err
		}
	}
	return nil
}

// TFEnable enables SST-TF for this package.
func (p *Package) TFEnable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.tfSetStatus(p.pkg, pu, true); err != nil {
			return err
		}
	}
	return nil
}

// TFDisable disables SST-TF for this package.
func (p *Package) TFDisable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.tfSetStatus(p.pkg, pu, false); err != nil {
			return err
		}
	}
	return nil
}

// CPEnable enables SST-CP for this package.
func (p *Package) CPEnable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetStatus(p.pkg, pu, true); err != nil {
			return err
		}
	}
	return nil
}

// CPDisable disables SST-CP for this package.
func (p *Package) CPDisable() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetStatus(p.pkg, pu, false); err != nil {
			return err
		}
	}
	return nil
}

// CPReset resets all CLOS parameters to defaults and associates all CPUs to CLOS 0.
func (p *Package) CPReset() error {
	for _, pu := range p.pkg.punits {
		if err := p.h.closResetConfiguration(p.pkg, pu); err != nil {
			return err
		}
		for cpu := range pu.cpus {
			if err := p.h.closAssociate(p.pkg, cpu, 0); err != nil {
				return fmt.Errorf("failed to associate cpu %d to clos 0: %w", cpu, err)
			}
		}
	}
	return nil
}

// CPSetPriorityType sets the SST-CP priority type for this package.
func (p *Package) CPSetPriorityType(priority CPPriorityType) error {
	if !priority.Validate() {
		return fmt.Errorf("invalid CP priority type %d: must be Proportional (0) or Ordered (1)", priority)
	}
	for _, pu := range p.pkg.punits {
		if err := p.h.cpSetPriorityType(p.pkg, pu, priority); err != nil {
			return err
		}
	}
	return nil
}

// ClosConfigure configures CLOS parameters for this package.
func (p *Package) ClosConfigure(clos int, config ClosConfig) error {
	for _, pu := range p.pkg.punits {
		if err := p.h.closConfigure(p.pkg, pu, clos, &config); err != nil {
			return err
		}
	}
	return nil
}
