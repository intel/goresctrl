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

// This file is used for auto-generation of sst_types_amd64.go
package sst

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
type isstIfPlatformInfo C.struct_isst_if_platform_info

type isstIfCPUMaps C.struct_isst_if_cpu_maps
type isstIfCPUMap C.struct_isst_if_cpu_map

type isstIfIoReg C.struct_isst_if_io_reg
type isstIfIoRegs C.struct_isst_if_io_regs

type isstIfMboxCmd C.struct_isst_if_mbox_cmd
type isstIfMboxCmds C.struct_isst_if_mbox_cmds

// TPMI types
type isstTpmiInstanceCount C.struct_isst_tpmi_instance_count
type isstCorePower C.struct_isst_core_power
type isstClosParam C.struct_isst_clos_param
type isstIfClosAssoc C.struct_isst_if_clos_assoc
type isstIfClosAssocCmds C.struct_isst_if_clos_assoc_cmds
type isstPerfLevelInfo C.struct_isst_perf_level_info
type isstPerfLevelControl C.struct_isst_perf_level_control
type isstPerfFeatureControl C.struct_isst_perf_feature_control
type isstPerfLevelDataInfo C.struct_isst_perf_level_data_info
type isstPerfLevelCpuMask C.struct_isst_perf_level_cpu_mask
type isstBaseFreqInfo C.struct_isst_base_freq_info
type isstTurboFreqInfo C.struct_isst_turbo_freq_info
