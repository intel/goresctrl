// Copyright 2026 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build amd64

package isst

import (
	"fmt"
	"unsafe"
)

// SendMSRCmd reads or writes one MSR via the ISST interface.
// If write is false, data is populated with the MSR value on return.
func SendMSRCmd(cpu uint16, msr uint64, write bool, data *uint64) error {
	var rw uint32
	if write {
		rw = 1
	}
	req := MsrCmds{
		Cmd_count: 1,
		Msr_cmd: [1]MsrCmd{{
			Read_write:  rw,
			Logical_cpu: uint32(cpu),
			Msr:         msr,
			Data:        *data,
		}},
	}
	if err := Ioctl(ISST_IF_MSR_COMMAND, uintptr(unsafe.Pointer(&req))); err != nil {
		return fmt.Errorf("MSR 0x%x command for cpu %d: %w", msr, cpu, err)
	}
	*data = req.Msr_cmd[0].Data
	return nil
}
