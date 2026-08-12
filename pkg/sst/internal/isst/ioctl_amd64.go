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

package isst

import (
	"fmt"
	"os"
	"syscall"
)

// Ioctl executes an ioctl on the Linux isst_if device driver.
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
