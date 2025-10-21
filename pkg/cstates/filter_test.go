/*
Copyright 2025 Intel Corporation

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

package cstates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBasicFilter(t *testing.T) {
	tests := []struct {
		name           string
		filter         Filter
		input          *FilterInput
		expectedResult bool
	}{
		{
			name:           "empty filter, empty input",
			filter:         NewBasicFilter(),
			input:          NewFilterInput(),
			expectedResult: true,
		},
		{
			name:           "empty filter, non-empty input",
			filter:         NewBasicFilter().SetCPUs(1).SetCstateNames("C2").SetAttributes(AttrDisable).SetAttributeValues(AttrDisable, "true"),
			input:          NewFilterInput(),
			expectedResult: true,
		},
		{
			name:           "matching CPU",
			filter:         NewBasicFilter().SetCPUs(0, 1, 2, 3),
			input:          NewFilterInput().SetCPU(1),
			expectedResult: true,
		},
		{
			name:           "non-matching CPU",
			filter:         NewBasicFilter().SetCPUs(0, 1, 2, 3),
			input:          NewFilterInput().SetCPU(4),
			expectedResult: false,
		},
		{
			name:           "empty CPU set",
			filter:         NewBasicFilter().SetCPUs(),
			input:          NewFilterInput().SetCPU(0),
			expectedResult: false,
		},
		{
			name:           "matching C-state name",
			filter:         NewBasicFilter().SetCstateNames("C2", "C3"),
			input:          NewFilterInput().SetCstateName("C3"),
			expectedResult: true,
		},
		{
			name:           "non-matching C-state name",
			filter:         NewBasicFilter().SetCstateNames("C2", "C3"),
			input:          NewFilterInput().SetCstateName("C6"),
			expectedResult: false,
		},
		{
			name:           "empty set of C-state names",
			filter:         NewBasicFilter().SetCstateNames(),
			input:          NewFilterInput().SetCstateName("C2"),
			expectedResult: false,
		},
		{
			name:           "matching attribute",
			filter:         NewBasicFilter().SetAttributes(AttrDisable, AttrLatency),
			input:          NewFilterInput().SetAttribute(AttrLatency),
			expectedResult: true,
		},
		{
			name:           "non-matching attribute",
			filter:         NewBasicFilter().SetAttributes(AttrDisable, AttrLatency),
			input:          NewFilterInput().SetAttribute(AttrBelow),
			expectedResult: false,
		},
		{
			name:           "empty set of attributes",
			filter:         NewBasicFilter().SetAttributes(),
			input:          NewFilterInput().SetAttribute(AttrDisable),
			expectedResult: false,
		},
		{
			name:           "matching attribute value",
			filter:         NewBasicFilter().SetAttributeValues(AttrAbove, "100", "200"),
			input:          NewFilterInput().SetAttributeValue(AttrAbove, "200"),
			expectedResult: true,
		},
		{
			name:           "non-matching attribute value",
			filter:         NewBasicFilter().SetAttributeValues(AttrAbove, "100", "200"),
			input:          NewFilterInput().SetAttributeValue(AttrAbove, "300"),
			expectedResult: false,
		},
		{
			name:           "empty set of attribute values",
			filter:         NewBasicFilter().SetAttributeValues(AttrBelow),
			input:          NewFilterInput().SetAttributeValue(AttrBelow, "50"),
			expectedResult: false,
		},
		{
			name:           "empty filter sets, empty input",
			filter:         NewBasicFilter().SetCPUs().SetCstateNames().SetAttributes().SetAttributeValues(AttrDisable),
			input:          NewFilterInput(),
			expectedResult: true,
		},
		{
			name:           "multiple criteria all match",
			filter:         NewBasicFilter().SetCPUs(0, 1).SetCstateNames("C2", "C6").SetAttributes(AttrDisable).SetAttributeValues(AttrDisable, "true"),
			input:          NewFilterInput().SetCPU(1).SetCstateName("C2").SetAttributeValue(AttrDisable, "true"),
			expectedResult: true,
		},
		{
			name:           "multiple criteria one does not match",
			filter:         NewBasicFilter().SetCPUs(0, 1).SetCstateNames("C2", "C6").SetAttributes(AttrBelow).SetAttributeValues(AttrDisable, "true"),
			input:          NewFilterInput().SetCPU(1).SetCstateName("C2").SetAttributeValue(AttrDisable, "true"),
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.Match(tt.input)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
