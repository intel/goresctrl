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

import "github.com/intel/goresctrl/pkg/utils"

// CstatesFilter returns true for accepted value combinations.
type CstatesFilter func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool

// FilterCPUs returns a filter that accepts only the specified CPU IDs.
func FilterCPUs(ids ...utils.ID) CstatesFilter {
	idset := utils.NewIDSet(ids...)
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		return idset.Has(cpuid)
	}
}

// FilterAttrs returns a filter that accepts only the specified attribute IDs.
func FilterAttrs(attributes ...AttrID) CstatesFilter {
	attrs := make(map[AttrID]bool, len(attributes)+1)
	for _, attr := range attributes {
		attrs[attr] = true
	}
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		if attr == -1 {
			return true
		}
		return attrs[attr]
	}
}

// FilterNames returns a filter that accepts only the specified C-state names.
func FilterNames(cstateNames ...string) CstatesFilter {
	names := make(map[string]bool, len(cstateNames)+1)
	names[""] = true
	for _, name := range cstateNames {
		names[name] = true
	}
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		return names[cstateName]
	}
}

// FilterAttrValues returns a filter that accepts only the specified values for a given attribute ID.
func FilterAttrValues(attribute AttrID, values ...string) CstatesFilter {
	vals := make(map[string]bool, len(values))
	for _, v := range values {
		vals[v] = true
	}
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		if attr == -1 || val == nil {
			return true
		}
		return attr == attribute && vals[*val]
	}
}

// FilterAll returns a filter that accepts only if all the provided filters accept.
func FilterAll(filters ...CstatesFilter) CstatesFilter {
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		for _, f := range filters {
			if !f(cpuid, cstateName, attr, val) {
				return false
			}
		}
		return true
	}
}

// FilterAny returns a filter that accepts if any of the provided filters accept.
func FilterAny(filters ...CstatesFilter) CstatesFilter {
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		for _, f := range filters {
			if f(cpuid, cstateName, attr, val) {
				return true
			}
		}
		return false
	}
}

// FilterInverse returns a filter that inverts the result of the provided filter.
func FilterInverse(filter CstatesFilter) CstatesFilter {
	return func(cpuid utils.ID, cstateName string, attr AttrID, val *string) bool {
		return !filter(cpuid, cstateName, attr, val)
	}
}
