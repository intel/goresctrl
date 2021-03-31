// Copyright 2020-2021 Intel Corporation. All Rights Reserved.
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

package testutils

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"
)

// VerifyDeepEqual checks that two values (including structures) are equal, or else it fails the test.
func VerifyDeepEqual(t *testing.T, valueName string, expectedValue interface{}, seenValue interface{}) bool {
	if reflect.DeepEqual(expectedValue, seenValue) {
		return true
	}
	t.Errorf("expected %s value %+v, got %+v", valueName, expectedValue, seenValue)
	return false
}

// VerifyError checks a (multi)error has expected properties, or else it fails the test.
func VerifyError(t *testing.T, err error, expectedCount int, expectedSubstrings []string) bool {
	if expectedCount > 0 {
		if err == nil {
			t.Errorf("error expected, got nil")
			return false
		}
		merr, ok := err.(*multierror.Error)
		if !ok {
			if expectedCount > 1 {
				t.Errorf("expected %d errors, but got %#v instead of multierror", expectedCount, err)
				return false
			}
			// If exactly one error is expected, then err
			// is allowed to be any error, not just a
			// multierror.
		} else if len(merr.Errors) != expectedCount {
			t.Errorf("expected %d errors, but got %d: %v", expectedCount, len(merr.Errors), merr)
			return false
		}
	} else if expectedCount == 0 {
		if err != nil {
			t.Errorf("expected 0 errors, but got: %v", err)
			return false
		}
	}
	for _, substring := range expectedSubstrings {
		if !strings.Contains(err.Error(), substring) {
			t.Errorf("expected error with substring %#v, got \"%v\"", substring, err)
		}
	}
	return true
}

func VerifyNoError(t *testing.T, err error) bool {
	if err != nil {
		t.Errorf("expected no error, got %v", err)
		return false
	}
	return true
}

func VerifyStrings(t *testing.T, expected string, got string) bool {
	if expected != got {
		t.Errorf("Strings differ: expected %q, got %q", expected, got)
		return false
	}
	return true
}

func VerifyStringSlices(t *testing.T, expected []string, got []string) bool {
	if len(expected) != len(got) {
		t.Errorf("Expected string slice of length %d, got %d", len(expected), len(got))
		return false
	}
	for i, es := range expected {
		if es != got[i] {
			t.Errorf("Slices differ: expected[%d]=%q, got[%d]=%q", i, es, i, got[i])
			return false
		}
	}
	return true
}
