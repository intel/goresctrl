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

type FilterInputOpt func(*FilterInput)

// Filter defines the interface for filtering cstate data.
type Filter interface {
	// Match returns true if the given arguments match the filter criteria.
	Match(*FilterInput) bool
}

// FilterInput holds the input values for evaluating the filter. All fields are
// optional. Nil values indicate "not defined" and should be ignored in filter
// evaluation.
type FilterInput struct {
	CPU            *utils.ID
	CstateName     *string
	Attribute      *AttrID
	AttributeValue *string
}

// BasicFilter implements a simple filter based on CPU IDs, cstate names and attributes.
type BasicFilter struct {
	cpus            map[utils.ID]bool
	cstateNames     map[string]bool
	attributes      map[AttrID]bool
	attributeValues map[AttrID]map[string]bool
}

func NewBasicFilter() *BasicFilter {
	return &BasicFilter{}
}

// SetCPUs sets the CPU IDs accepted by the filter.
func (f *BasicFilter) SetCPUs(cpus ...utils.ID) *BasicFilter {
	f.cpus = make(map[utils.ID]bool, len(cpus))
	for _, cpu := range cpus {
		f.cpus[cpu] = true
	}
	return f
}

// SetCstateNames sets the cstate names accepted by the filter.
func (f *BasicFilter) SetCstateNames(names ...string) *BasicFilter {
	f.cstateNames = make(map[string]bool, len(names))
	for _, name := range names {
		f.cstateNames[name] = true
	}
	return f
}

// SetAttributes sets the attribute IDs accepted by the filter.
func (f *BasicFilter) SetAttributes(attrs ...AttrID) *BasicFilter {
	f.attributes = make(map[AttrID]bool, len(attrs))
	for _, attr := range attrs {
		f.attributes[attr] = true
	}
	return f
}

// SetAttributeValues sets the values of a given attribute accepted by the filter.
// This does not imply that the attribute itself is accepted or other
// attributes would be rejected; use SetAttributes for filtering attribute IDs.
func (f *BasicFilter) SetAttributeValues(attr AttrID, values ...string) *BasicFilter {
	if f.attributeValues == nil {
		f.attributeValues = make(map[AttrID]map[string]bool)
	}
	f.attributeValues[attr] = make(map[string]bool, len(values))
	for _, value := range values {
		f.attributeValues[attr][value] = true
	}
	return f
}

// Match evaluates the filter against the provided input arguments. Implements a logical AND of all filter criteria.
func (f *BasicFilter) Match(args *FilterInput) bool {
	return f.evaluateCPU(args.CPU) && f.evaluateCstateName(args.CstateName) && f.evaluateAttribute(args.Attribute) && f.evaluateAttributeValue(args.Attribute, args.AttributeValue)
}

func (f *BasicFilter) evaluateCPU(cpu *utils.ID) bool {
	return cpu == nil || f.cpus == nil || f.cpus[*cpu]
}

func (f *BasicFilter) evaluateCstateName(name *string) bool {
	return name == nil || f.cstateNames == nil || f.cstateNames[*name]
}

func (f *BasicFilter) evaluateAttribute(attr *AttrID) bool {
	return attr == nil || f.attributes == nil || f.attributes[*attr]
}

func (f *BasicFilter) evaluateAttributeValue(attr *AttrID, value *string) bool {
	return attr == nil || value == nil || f.attributeValues == nil || f.attributeValues[*attr] == nil || f.attributeValues[*attr][*value]
}

// NewFilterInput creates a new FilterInput instance.
func NewFilterInput(opts ...FilterInputOpt) *FilterInput {
	return &FilterInput{}
}

// SetCPU sets the CPU ID in the FilterInput.
func (f *FilterInput) SetCPU(cpu utils.ID) *FilterInput {
	f.CPU = &cpu
	return f
}

// SetCstateName sets the cstate name in the FilterInput.
func (f *FilterInput) SetCstateName(name string) *FilterInput {
	f.CstateName = &name
	return f
}

// SetAttribute sets the attribute ID in the FilterInput. It clears any
// existing AttributeValue causing the filter to match any value for the
// attribute.
func (f *FilterInput) SetAttribute(attr AttrID) *FilterInput {
	f.Attribute = &attr
	f.AttributeValue = nil
	return f
}

// SetAttributeValue sets the attribute ID and value in the FilterInput.
func (f *FilterInput) SetAttributeValue(attr AttrID, value string) *FilterInput {
	f.Attribute = &attr
	f.AttributeValue = &value
	return f
}
