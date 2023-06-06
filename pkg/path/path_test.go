/*
Copyright 2023 Intel Corporation

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

package path

import (
	"testing"
)

func TestPath(t *testing.T) {
	// Helper function for checking test cases
	TC := func(path []string, expected string) {
		if result := Path(path...); result != expected {
			t.Errorf("unexpected path: %v -> %q, expected %q", path, result, expected)
		}
	}

	// Run test cases
	TC([]string{}, "/")
	TC([]string{"foo"}, "/foo")

	SetPrefix("/prefix/mnt/")
	TC([]string{}, "/prefix/mnt")
	TC([]string{"/foo", "bar"}, "/prefix/mnt/foo/bar")
}
