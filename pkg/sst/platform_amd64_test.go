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
	"os"
	"path/filepath"
	"testing"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
)

func TestSstSupported(t *testing.T) {
	root := t.TempDir()
	goresctrlpath.SetPrefix(root)
	t.Cleanup(func() { goresctrlpath.SetPrefix("/") })

	if SstSupported() {
		t.Fatal("SstSupported returned true with no isst device")
	}

	devDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(devDir, 0o755); err != nil {
		t.Fatalf("failed to create fake dev directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "isst_interface"), nil, 0o600); err != nil {
		t.Fatalf("failed to create fake isst device: %v", err)
	}

	if !SstSupported() {
		t.Fatal("SstSupported returned false with an isst device")
	}
}
