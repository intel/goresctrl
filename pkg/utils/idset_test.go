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

package utils

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDSetString(t *testing.T) {
	tcs := []struct {
		name string
		ids  []int
		want string
	}{
		{
			name: "empty",
			ids:  nil,
			want: "",
		},
		{
			name: "single",
			ids:  []int{5},
			want: "5",
		},
		{
			name: "two non-adjacent",
			ids:  []int{1, 3},
			want: "1,3",
		},
		{
			name: "full range",
			ids:  []int{0, 1, 2, 3, 4, 5},
			want: "0-5",
		},
		{
			name: "two-element range",
			ids:  []int{3, 4},
			want: "3-4",
		},
		{
			name: "range then single",
			ids:  []int{0, 1, 2, 5},
			want: "0-2,5",
		},
		{
			name: "single then range",
			ids:  []int{0, 2, 3, 4},
			want: "0,2-4",
		},
		{
			name: "mixed ranges and singles",
			ids:  []int{0, 2, 3, 4, 7, 8, 10},
			want: "0,2-4,7-8,10",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := NewIDSetFromIntSlice(tc.ids...).String()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIDSetMarshalJSON(t *testing.T) {
	tcs := []struct {
		name string
		ids  []int
		want string
	}{
		{
			name: "empty",
			ids:  nil,
			want: `""`,
		},
		{
			name: "single",
			ids:  []int{7},
			want: `"7"`,
		},
		{
			name: "range",
			ids:  []int{0, 1, 2, 3},
			want: `"0-3"`,
		},
		{
			name: "mixed",
			ids:  []int{0, 2, 3, 4, 7, 8, 10},
			want: `"0,2-4,7-8,10"`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(NewIDSetFromIntSlice(tc.ids...))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(data))
		})
	}
}

func TestIDSetUnmarshalJSON(t *testing.T) {
	tcs := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{
			name:  "empty string",
			input: `""`,
			want:  nil,
		},
		{
			name:  "single",
			input: `"5"`,
			want:  []int{5},
		},
		{
			name:  "comma list",
			input: `"1,3,5"`,
			want:  []int{1, 3, 5},
		},
		{
			name:  "range",
			input: `"0-3"`,
			want:  []int{0, 1, 2, 3},
		},
		{
			name:  "mixed",
			input: `"0,2-4,7-8,10"`,
			want:  []int{0, 2, 3, 4, 7, 8, 10},
		},
		{
			name:    "not a string",
			input:   `42`,
			wantErr: true,
		},
		{
			name:    "inverted range",
			input:   `"3-1"`,
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   `"a,b"`,
			wantErr: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			var got IDSet
			err := json.Unmarshal([]byte(tc.input), &got)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, NewIDSetFromIntSlice(tc.want...), got)
			}
		})
	}
}
