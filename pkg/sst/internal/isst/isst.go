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

package isst

//go:generate ./gen_types.sh

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"unsafe"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
)

var log *slog.Logger = slog.Default()

// SetLogger sets the logger instance used by the package.
func SetLogger(l *slog.Logger) { log = l }

// DevPath returns the path to the isst_interface device.
func DevPath() string { return goresctrlpath.Path("dev/isst_interface") }

// Ioctl executes an ioctl on the linux isst_if device driver.
func Ioctl(ioctl uintptr, req uintptr) error {
	devPath := DevPath()
	f, err := os.Open(devPath)
	if err != nil {
		return fmt.Errorf("failed to open isst device %q: %v", devPath, err)
	}
	defer f.Close() //nolint:errcheck
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(f.Fd()), ioctl, req); errno != 0 {
		return errno
	}
	return nil
}

var (
	cpuMapMu sync.RWMutex
	cpuMap   = make(map[uint16]uint16)
)

// GetPunitCPUId returns the PUNIT CPU id for a given Linux logical CPU.
func GetPunitCPUId(cpu uint16) (uint16, error) {
	cpuMapMu.RLock()
	id, ok := cpuMap[cpu]
	cpuMapMu.RUnlock()
	if ok {
		return id, nil
	}
	id, err := getCPUMapping(cpu)
	if err != nil {
		return 0, err
	}
	cpuMapMu.Lock()
	cpuMap[cpu] = id
	cpuMapMu.Unlock()
	return id, nil
}

func getCPUMapping(cpu uint16) (uint16, error) {
	req := CPUMaps{
		Cmd_count: 1,
		Cpu_map:   [1]CPUMap{{Logical_cpu: uint32(cpu)}},
	}
	if err := Ioctl(ISST_IF_GET_PHY_ID, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("failed to get CPU mapping for cpu %d: %v", cpu, err)
	}
	return uint16(req.Cpu_map[0].Physical_cpu), nil
}

// SendMboxCmd sends one mailbox command to PUNIT.
func SendMboxCmd(cpu uint16, cmd uint16, subCmd uint16, parameter uint32, reqData uint32) (uint32, error) {
	req := MboxCmds{
		Cmd_count: 1,
		Mbox_cmd: [1]MboxCmd{{
			Logical_cpu: uint32(cpu),
			Command:     cmd,
			Sub_command: subCmd,
			Parameter:   parameter,
			Req_data:    reqData,
		}},
	}
	log.Debug("MBOX SEND", "cpu", cpu, "cmd", cmd, "subCmd", subCmd, SlogHex("data", reqData))
	if err := Ioctl(ISST_IF_MBOX_COMMAND, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("mbox command failed with %v", err)
	}
	log.Debug("MBOX RECV", SlogHex("data", req.Mbox_cmd[0].Resp_data))
	return req.Mbox_cmd[0].Resp_data, nil
}

// SendMMIOCmd sends one MMIO command to PUNIT.
func SendMMIOCmd(cpu uint16, reg uint32, value uint32, doWrite bool) (uint32, error) {
	var ReadWrite uint32
	if doWrite {
		ReadWrite = 1
	}
	req := IoRegs{
		Req_count: 1,
		Io_reg: [1]IoReg{{
			Logical_cpu: uint32(cpu),
			Reg:         reg,
			Value:       value,
			Read_write:  ReadWrite,
		}},
	}
	log.Debug("MMIO SEND", "cpu", cpu, "reg", reg, SlogHex("data", value), "write", doWrite)
	if err := Ioctl(ISST_IF_IO_CMD, uintptr(unsafe.Pointer(&req))); err != nil {
		return 0, fmt.Errorf("MMIO command failed with %v", err)
	}
	log.Debug("MMIO RECV", SlogHex("data", req.Io_reg[0].Value))
	return req.Io_reg[0].Value, nil
}

// SlogHex returns a slog.Attr with a hex-formatted uint32.
func SlogHex(key string, val uint32) slog.Attr {
	return slog.String(key, fmt.Sprintf("%#02x", val))
}

// GetPlatformAPIVersion gets the ISST API version.
func GetPlatformAPIVersion() (int, error) {
	var info PlatformInfo
	if err := Ioctl(ISST_IF_GET_PLATFORM_INFO, uintptr(unsafe.Pointer(&info))); err != nil {
		return 0, fmt.Errorf("failed to get ISST platform info: %w", err)
	}
	return int(info.Api_version), nil
}
