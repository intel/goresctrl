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
	// Match returns true if the given arguments match the filter criteria
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
	cpus        map[utils.ID]bool
	cstateNames map[string]bool
	attributes  map[AttrID]map[string]bool
}

func NewBasicFilter() *BasicFilter {
	return &BasicFilter{}
}

// SetCPUs sets the CPU IDs accepted by the filter.
func (f *BasicFilter) SetCPUs(cpus ...utils.ID) *BasicFilter {
	f.cpus = (make(map[utils.ID]bool, len(cpus)))
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
	f.attributes = make(map[AttrID]map[string]bool, len(attrs))
	for _, attr := range attrs {
		if f.attributes[attr] == nil {
			f.attributes[attr] = make(map[string]bool)
		}
	}
	return f
}

// SetAttributeValues sets the values of a given attribute accepted by the filter.
func (f *BasicFilter) SetAttributeValues(attr AttrID, values ...string) *BasicFilter {
	if f.attributes == nil {
		f.attributes = make(map[AttrID]map[string]bool)
	}
	f.attributes[attr] = make(map[string]bool, len(values))
	for _, value := range values {
		f.attributes[attr][value] = true
	}
	return f
}

// Match evaluates the filter against the provided input arguments. Implements a logical AND of all filter criteria.
func (f *BasicFilter) Match(args *FilterInput) bool {
	return f.evaluateCPU(args.CPU) && f.evaluateCstateName(args.CstateName) && f.evaluateAttribute(args.Attribute) && f.evaluateAttributeValue(args.Attribute, args.AttributeValue)
}

func (f *BasicFilter) evaluateCPU(cpu *utils.ID) bool {
	return cpu == nil || len(f.cpus) == 0 || f.cpus[*cpu]
}

func (f *BasicFilter) evaluateCstateName(name *string) bool {
	return name == nil || len(f.cstateNames) == 0 || f.cstateNames[*name]
}

func (f *BasicFilter) evaluateAttribute(attr *AttrID) bool {
	return attr == nil || len(f.attributes) == 0 || f.attributes[*attr] != nil
}

func (f *BasicFilter) evaluateAttributeValue(attr *AttrID, value *string) bool {
	return attr == nil || value == nil || len(f.attributes) == 0 || len(f.attributes[*attr]) == 0 || f.attributes[*attr][*value]
}

// NewFilterInput creates a new FilterInput instance.
func NewFilterInput(opts ...FilterInputOpt) *FilterInput {
	f := &FilterInput{}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// WithCPU sets the CPU ID in the FilterInput.
func WithCPU(cpu utils.ID) FilterInputOpt {
	return func(f *FilterInput) {
		f.CPU = &cpu
	}
}

// WithCstateName sets the cstate name in the FilterInput.
func WithCstateName(name string) FilterInputOpt {
	return func(f *FilterInput) {
		f.CstateName = &name
	}
}

// WithAttribute sets the attribute ID in the FilterInput.
func WithAttribute(attr AttrID) FilterInputOpt {
	return func(f *FilterInput) {
		f.Attribute = &attr
		f.AttributeValue = nil
	}
}

// WithAttributeValue sets the attribute ID and value in the FilterInput.
func WithAttributeValue(attr AttrID, value string) FilterInputOpt {
	return func(f *FilterInput) {
		f.Attribute = &attr
		f.AttributeValue = &value
	}
}
