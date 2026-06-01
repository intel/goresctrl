//go:build amd64
// +build amd64

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

// This file is used for auto-generation of types_msr_amd64.go.
// Regenerate with: go generate ./pkg/sst/...
// Requires kernel source tree at $KERNEL_SRC_DIR (default: /usr/src/linux).
package isst

// #include <linux/isst_if.h>
// #include <linux/ioctl.h>
//
import "C"

const (
	ISST_IF_MSR_COMMAND = C.ISST_IF_MSR_COMMAND
)

type MsrCmd C.struct_isst_if_msr_cmd
type MsrCmds C.struct_isst_if_msr_cmds
