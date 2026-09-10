//go:build !amd64

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
	"os"
	"path/filepath"
	"testing"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
)

func TestIoctlUnsupportedArchitecture(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatalf("failed to create fake dev directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "isst_interface"), nil, 0o600); err != nil {
		t.Fatalf("failed to create fake isst device: %v", err)
	}
	goresctrlpath.SetPrefix(root)
	t.Cleanup(func() { goresctrlpath.SetPrefix("/") })

	if err := Ioctl(0, 0); err != errUnsupportedArchitecture {
		t.Fatalf("Ioctl returned unexpected error on a non-amd64 architecture: %v", err)
	}
}
