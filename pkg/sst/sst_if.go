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

//go:generate ./gen_sst_types.sh

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"syscall"
	"unsafe"

	"github.com/intel/goresctrl/pkg/utils"
)

// cpuMap holds the logical to punit cpu mapping table
var cpuMap = make(map[utils.ID]utils.ID)

// punitCPU returns the PUNIT CPU id corresponding a given Linux logical CPU
func punitCPU(cpu utils.ID) (utils.ID, error) {
	if id, ok := cpuMap[cpu]; ok {
		return id, nil
	}

	id, err := getCPUMapping(cpu)
	if err == nil {
		cpuMap[cpu] = id
	}
	return id, err
}

// isstIoctl is a helper for executing ioctls on the linux isst_if device driver
func isstIoctl(ioctl uintptr, req uintptr) error {
	devPath := isstDevPath()
	f, err := os.Open(devPath)
	if err != nil {
		return fmt.Errorf("failed to open isst device %q: %v", devPath, err)
	}
	defer f.Close() // nolint:errcheck

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), ioctl, req); errno != 0 {
		return errno
	}

	return nil
}

// getCPUMapping gets mapping of Linux logical CPU numbers to (package-specific)
// PUNIT CPU number for one cpu. This is needed because the PUNIT CPU/core
// numbering differs from the Linux kernel numbering (exposed via sysfs) which
// is based on APIC.
func getCPUMapping(cpu utils.ID) (utils.ID, error) {
	if cpu < 0 || cpu > math.MaxUint32 {
		return utils.Unknown, fmt.Errorf("invalid CPU number %d", cpu)
	}

	req := isstIfCPUMaps{
		Cmd_count: 1,
		Cpu_map: [1]isstIfCPUMap{
			{Logical_cpu: uint32(cpu)},
		},
	}

	if err := isstIoctl(ISST_IF_GET_PHY_ID, uintptr(unsafe.Pointer(&req))); err != nil {
		return -1, fmt.Errorf("failed to get CPU mapping for cpu %d: %v", cpu, err)
	}

	return utils.ID(req.Cpu_map[0].Physical_cpu), nil
}

// sendMboxCmd sends one mailbox command to PUNIT
func sendMboxCmd(cpu utils.ID, cmd uint16, subCmd uint16, parameter uint32, reqData uint32) (uint32, error) {
	if cpu < 0 || cpu > math.MaxUint32 {
		return 0, fmt.Errorf("invalid CPU number %d", cpu)
	}

	req := isstIfMboxCmds{
		Cmd_count: 1,
		Mbox_cmd: [1]isstIfMboxCmd{
			{
				Logical_cpu: uint32(cpu),
				Command:     cmd,
				Sub_command: subCmd,
				Parameter:   parameter,
				Req_data:    reqData,
			},
		},
	}

	sstlog.Debug("MBOX SEND", "cpu", cpu, "cmd", cmd, "subCmd", subCmd, slogHex("data", reqData))
	if err := isstIoctl(ISST_IF_MBOX_COMMAND, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("mbox command failed with %v", err)
	}
	sstlog.Debug("MBOX RECV", slogHex("data", req.Mbox_cmd[0].Resp_data))

	return req.Mbox_cmd[0].Resp_data, nil
}

// sendMMIOCmd sends one MMIO command to PUNIT
func sendMMIOCmd(cpu utils.ID, reg uint32, value uint32, doWrite bool) (uint32, error) {
	if cpu < 0 || cpu > math.MaxUint32 {
		return 0, fmt.Errorf("invalid CPU number %d", cpu)
	}

	var ReadWrite uint32

	if doWrite {
		ReadWrite = 1
	}

	req := isstIfIoRegs{
		Req_count: 1,
		Io_reg: [1]isstIfIoReg{
			{
				Logical_cpu: uint32(cpu),
				Reg:         reg,
				Value:       value,
				Read_write:  ReadWrite,
			},
		},
	}
	sstlog.Debug("MMIO SEND", "cpu", cpu, "reg", reg, slogHex("data", value), "write", doWrite)
	if err := isstIoctl(ISST_IF_IO_CMD, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("MMIO command failed with %v", err)
	}
	sstlog.Debug("MMIO RECV", slogHex("data", req.Io_reg[0].Value))

	return req.Io_reg[0].Value, nil
}

func slogHex(key string, val uint32) slog.Attr {
	return slog.String(key, fmt.Sprintf("%#02x", val))
}
