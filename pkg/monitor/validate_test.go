//go:build linux

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

package monitor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPodUIDValidator(t *testing.T) {
	tests := []struct {
		name string
		key  string
		ok   bool
	}{
		{"valid lowercase", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", true},
		{"valid uppercase", "A1B2C3D4-E5F6-7890-ABCD-EF1234567890", true},
		{"valid mixed", "a1B2c3D4-e5F6-7890-AbCd-eF1234567890", true},
		{"valid compact lowercase", "a1b2c3d4e5f67890abcdef1234567890", true},
		{"valid compact uppercase", "A1B2C3D4E5F67890ABCDEF1234567890", true},
		{"too short", "a1b2c3d4-e5f6-7890-abcd-ef123456789", false},
		{"too long", "a1b2c3d4-e5f6-7890-abcd-ef12345678901", false},
		{"wrong separator count", "a1b2c3d4e5f6-7890-abcd-ef1234567890", false},
		{"compact too short", "a1b2c3d4e5f67890abcdef123456789", false},
		{"compact too long", "a1b2c3d4e5f67890abcdef1234567890a", false},
		{"compact invalid char", "g1b2c3d4e5f67890abcdef1234567890", false},
		{"invalid char g", "g1b2c3d4-e5f6-7890-abcd-ef1234567890", false},
		{"empty", "", false},
		{"short segment", "a1b2c3d-e5f6-7890-abcd-ef1234567890", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.ok, PodUIDValidator(tc.key))
		})
	}
}

func TestCanonicalizePodUID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"compact lowercase", "a1b2c3d4e5f67890abcdef1234567890", "a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
		{"compact uppercase", "A1B2C3D4E5F67890ABCDEF1234567890", "a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
		{"already dashed", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", "a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
		{"dashed uppercase lowercased", "A1B2C3D4-E5F6-7890-ABCD-EF1234567890", "a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
		{"non-uuid 32 non-hex untouched", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
		{"other length untouched", "short", "short"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CanonicalizePodUID(tc.in))
		})
	}
}

func TestIsHex(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", false},
		{"lowercase hex", "abcdef0123456789", true},
		{"uppercase hex", "ABCDEF0123456789", true},
		{"non-hex", "xyz", false},
		{"mixed non-hex", "abcg", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isHex(tc.in))
		})
	}
}

func TestIsValidRDTClass(t *testing.T) {
	tests := []struct {
		name string
		cls  string
		ok   bool
	}{
		{"simple", "BestEffort", true},
		{"with dash", "my-class", true},
		{"with underscore", "my_class", true},
		{"empty", "", false},
		{"dot", ".", false},
		{"dotdot", "..", false},
		{"slash", "foo/bar", false},
		{"nul", "foo\x00bar", false},
		{"reserved mon_groups", "mon_groups", false},
		{"reserved mon_data", "mon_data", false},
		{"reserved info", "info", false},
		{"mon_ prefix allowed", "mon_control_group", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.ok, isValidRDTClass(tc.cls))
		})
	}
}

func TestValidate(t *testing.T) {
	// Use the existing testdata/mon_data tree which has:
	//   mon_L3_00/{llc_occupancy, mbm_total_bytes}
	//   mon_L3_01/{llc_occupancy}
	//   mon_PERF_PKG_00/{activity, core_energy}
	names, err := Validate("testdata")
	require.NoError(t, err)

	// Should discover the union of counter file names, sorted.
	expected := []string{"activity", "core_energy", "llc_occupancy", "mbm_total_bytes"}
	assert.Equal(t, expected, names)
}

func TestValidate_NotExist(t *testing.T) {
	_, err := Validate("/nonexistent/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mon_data not available")
}

func TestValidate_EmptyMonData(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "mon_data"), 0755))
	_, err := Validate(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no counters found")
}
