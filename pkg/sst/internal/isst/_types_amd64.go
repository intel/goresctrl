//go:build amd64
// +build amd64

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

// This file is used for auto-generation of types_amd64.go
package isst

// #include <linux/isst_if.h>
// #include <linux/ioctl.h>
//
import "C"

const (
	// Mbox/MMIO IOCTLs (API version 1)
	ISST_IF_GET_PLATFORM_INFO = C.ISST_IF_GET_PLATFORM_INFO
	ISST_IF_GET_PHY_ID        = C.ISST_IF_GET_PHY_ID
	ISST_IF_IO_CMD            = C.ISST_IF_IO_CMD
	ISST_IF_MBOX_COMMAND      = C.ISST_IF_MBOX_COMMAND

	// TPMI IOCTLs (API version 2+)
	ISST_IF_COUNT_TPMI_INSTANCES    = C.ISST_IF_COUNT_TPMI_INSTANCES
	ISST_IF_CORE_POWER_STATE        = C.ISST_IF_CORE_POWER_STATE
	ISST_IF_CLOS_PARAM              = C.ISST_IF_CLOS_PARAM
	ISST_IF_CLOS_ASSOC              = C.ISST_IF_CLOS_ASSOC
	ISST_IF_PERF_LEVELS             = C.ISST_IF_PERF_LEVELS
	ISST_IF_PERF_SET_LEVEL          = C.ISST_IF_PERF_SET_LEVEL
	ISST_IF_PERF_SET_FEATURE        = C.ISST_IF_PERF_SET_FEATURE
	ISST_IF_GET_PERF_LEVEL_INFO     = C.ISST_IF_GET_PERF_LEVEL_INFO
	ISST_IF_GET_PERF_LEVEL_CPU_MASK = C.ISST_IF_GET_PERF_LEVEL_CPU_MASK
	ISST_IF_GET_BASE_FREQ_INFO      = C.ISST_IF_GET_BASE_FREQ_INFO
	ISST_IF_GET_BASE_FREQ_CPU_MASK  = C.ISST_IF_GET_BASE_FREQ_CPU_MASK
	ISST_IF_GET_TURBO_FREQ_INFO     = C.ISST_IF_GET_TURBO_FREQ_INFO
)

// Mbox/MMIO types
type PlatformInfo C.struct_isst_if_platform_info

type CPUMaps C.struct_isst_if_cpu_maps
type CPUMap C.struct_isst_if_cpu_map

type IoReg C.struct_isst_if_io_reg
type IoRegs C.struct_isst_if_io_regs

type MboxCmd C.struct_isst_if_mbox_cmd
type MboxCmds C.struct_isst_if_mbox_cmds

// TPMI types
type TpmiInstanceCount C.struct_isst_tpmi_instance_count
type CorePower C.struct_isst_core_power
type ClosParam C.struct_isst_clos_param
type ClosAssoc C.struct_isst_if_clos_assoc
type ClosAssocCmds C.struct_isst_if_clos_assoc_cmds
type PerfLevelInfo C.struct_isst_perf_level_info
type PerfLevelControl C.struct_isst_perf_level_control
type PerfFeatureControl C.struct_isst_perf_feature_control
type PerfLevelDataInfo C.struct_isst_perf_level_data_info
type PerfLevelCpuMask C.struct_isst_perf_level_cpu_mask
type BaseFreqInfo C.struct_isst_base_freq_info
type TurboFreqInfo C.struct_isst_turbo_freq_info
