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

// Package cstates implements an API to read and modify CPU C-states
// in Linyux via the cpuidle interface in sysfs, typically located at
// /sys/devices/system/cpu/cpu*/cpuidle/state*.
//
// C-states (or idle states) are low-power states that a CPU can enter when it is idle.
// The deeper the C-state, the more power savings it provides, but also the longer the
// latency to return to an active state.
//
// In the API, a `Cstate` is a low-level representation of a single
// C-state of a single CPU. A `Cstates` instance is a collection of `Cstate`
// instances, representing one or more C-states of one or more CPUs. The collection
// can be filtered, modified, and applied back to sysfs.
//
// Basic usage examples
//
// 1. List disabled C-states for all CPUs
//
//    cs, err := cstates.NewCstatesFromSysfs(
//        cstates.FilterAttrValues(cstates.AttrDisable, "1"),
//    )
//    if err != nil {
//        log.Fatal(err)
//    }
//    for _, name := range cs.Names() {
//        fmt.Printf("%s disabled in %s\n", name, cs.Copy(cstates.FilterNames(name)).CPUs())
//    }
//
// 2. Disable C6, C8 and C10 C-states for CPUs 0-3
//
//    cs, err := cstates.NewCstatesFromSysfs(
//        cstates.FilterCPUs(0, 1, 2, 3),
//        cstates.FilterNames("C6", "C8", "C10"),
//        cstates.FilterAttrs(cstates.AttrDisable),
//    )
//    if err != nil {
//        log.Fatal(err)
//    }
//    cs.SetAttrs(cstates.AttrDisable, "1")
//    if err := cs.Apply(); err != nil {
//        log.Fatal(err)
//    }

package cstates

import (
	"fmt"
	"slices"
	"strings"

	"github.com/intel/goresctrl/pkg/utils"
)

// CstateAttribute represents a single C-state attribute, e.g. "residency".
type CstateAttribute struct {
	id       AttrID // attribute ID, e.g. AttrResidency
	name     string // attribute filename in sysfs cpuidle/state<NUM>/, e.g. "residency"
	readOnly bool   // true if attribute is read-only and must not be written to sysfs
}

// Cstate represents single C-state, e.g. "C6" of a single CPU, e.g. cpu2.
//
// Attribute values correspond to files located in sysfs. For example,
// Cstate{cpu: 2, state: 3}.attrVal[AttrResidency] corresponds to the file
// /sys/devices/system/cpu/cpu2/cpuidle/state3/residency.
//
// If an attribute value is nil, file has not been read value has been
// cleared to skip writing it to sysfs.
type Cstate struct {
	// The C-state name, e.g. "C6", read from the file cpuidle/state*/name.
	name string
	// The CPU number from the directory /sys/devices/system/cpu/cpu<NUM>.
	cpu utils.ID
	// The state number from the directory /sys/devices/system/cpu/cpu*/cpuidle/state<NUM>.
	state int

	// Attribute values, see Attributes for details.
	attrVal [AttrIDCount]*string
}

// Cstates represents a collection of Cstate instances.
type Cstates struct {
	fs       sysfsIface
	maxState int
	cstates  []*Cstate
}

type AttrID int

const (
	// C-state attributes IDs
	AttrAbove AttrID = iota
	AttrBelow
	AttrDisable
	AttrLatency
	AttrResidency
	AttrTime
	AttrIDCount
)

var (
	attributes = []CstateAttribute{
		{id: AttrAbove, name: "above", readOnly: true},
		{id: AttrBelow, name: "below", readOnly: true},
		{id: AttrDisable, name: "disable"},
		{id: AttrLatency, name: "latency", readOnly: true},
		{id: AttrResidency, name: "residency", readOnly: true},
		{id: AttrTime, name: "time", readOnly: true},
	}
	attrNameToID map[string]AttrID
	attrIDToName map[AttrID]string
)

func init() {
	attrNameToID = make(map[string]AttrID, len(attributes))
	attrIDToName = make(map[AttrID]string, len(attributes))
	for idx, attr := range attributes {
		if idx != int(attr.id) {
			panic("CstateAttribute IDs are not sequential starting from 0")
		}
		attrNameToID[attr.name] = attr.id
		attrIDToName[attr.id] = attr.name
	}
}

func (attr AttrID) String() string {
	if attr < 0 || int(attr) >= len(attributes) {
		return fmt.Sprintf("AttrID(%d)", attr)
	}
	return attributes[attr].name
}

// NewCstate creates a new Cstate instance with the given name.
func NewCstate(name string, cpu utils.ID, state int) *Cstate {
	return &Cstate{name: name, cpu: cpu, state: state}
}

// Name returns the C-state name, e.g. "C6".
func (c *Cstate) Name() string {
	return c.name
}

// CPU returns the CPU ID, e.g. 2.
func (c *Cstate) CPU() utils.ID {
	return c.cpu
}

// State returns the C-state number, e.g. 3 for state3.
func (c *Cstate) State() int {
	return c.state
}

// SetAttr sets the attribute value. If value is nil, the attribute
// is cleared and will not be written to sysfs.
func (c *Cstate) SetAttr(id AttrID, value *string) {
	if id < 0 || id >= AttrIDCount {
		panic(fmt.Sprintf("SetAttr: invalid AttrID %d", id))
	}
	if attributes[id].readOnly && value != nil {
		panic(fmt.Sprintf("SetAttr: attribute %q is read-only, must not set a non-nil value", attributes[id].name))
	}
	c.setAttr(id, value)
}

func (c *Cstate) setAttr(id AttrID, value *string) {
	c.attrVal[id] = value
}

// GetAttr returns the attribute value, or nil if the attribute
// has not been read from sysfs or it has been cleared.
func (c *Cstate) GetAttr(id AttrID) *string {
	if id < 0 || id >= AttrIDCount {
		panic(fmt.Sprintf("GetAttr: invalid AttrID %d", id))
	}
	return c.attrVal[id]
}

// Attrs returns a list of attribute IDs that have a value set.
func (c *Cstate) Attrs() []AttrID {
	attrs := []AttrID{}
	for id, val := range c.attrVal {
		if val != nil {
			attrs = append(attrs, AttrID(id))
		}
	}
	return attrs
}

// AttrCount returns the number of attributes that have a value set.
func (c *Cstate) AttrCount() int {
	count := 0
	for _, val := range c.attrVal {
		if val != nil {
			count++
		}
	}
	return count
}

// Copy returns a new Cstate instance containing a copy attribute
// values. If filters are given, only matching attributes will be
// copied and while others will be nil.
func (c *Cstate) Copy(filters ...CstatesFilter) *Cstate {
	newC := NewCstate(c.name, c.cpu, c.state)
	allFilters := FilterAll(filters...)
	for attr, val := range c.attrVal {
		if val == nil {
			continue
		}
		if !allFilters(c.cpu, c.name, AttrID(attr), val) {
			continue
		}
		// no need to copy contents of an immutable string
		newC.attrVal[attr] = val
	}
	return newC
}

// String returns a string representation of the Cstate instance.
func (c *Cstate) String() string {
	attrs := []string{}
	for _, attr := range attributes {
		if c.attrVal[attr.id] != nil {
			attrs = append(attrs, fmt.Sprintf("%s=%s", attr.id, *c.attrVal[attr.id]))
		}
	}
	return fmt.Sprintf("Cstate{cpu%d state%d=%s %s}", c.cpu, c.state, c.name, strings.Join(attrs, " "))
}

// NewCstates creates a new empty Cstates instance.
func NewCstates() *Cstates {
	return &Cstates{
		maxState: -1,
		cstates:  []*Cstate{},
	}
}

// SetFs sets the filesystem interface to be used by the Cstates instance.
func (c *Cstates) SetFs(fs sysfsIface) {
	c.fs = fs
}

// NewCstatesFromSysfs creates a new Cstates instance by reading the
// C-state information from sysfs.
//
// If filters are given, only matching C-states and attributes are read.
func NewCstatesFromSysfs(filters ...CstatesFilter) (*Cstates, error) {
	cs := NewCstates()
	cs.SetFs(NewSysfs())
	if err := cs.Read(filters...); err != nil {
		return nil, err
	}
	return cs, nil
}

// Read populates the Cstates instance from filesystem that
// implements the sysfsIface interface.
func (cs *Cstates) Read(filters ...CstatesFilter) error {
	// stateName maps state to C-state name, e.g. 2 -> "C1E", if cpuidle/state2/name == "C1E"
	var stateName map[int]string
	cpusStr, err := cs.fs.PossibleCpus()
	if err != nil {
		return err
	}
	cpus, err := utils.NewIDSetFromString(cpusStr)
	if err != nil {
		return err
	}

	allFilters := FilterAll(filters...)

	for _, cpu := range cpus.SortedMembers() {
		if !allFilters(cpu, "", -1, nil) {
			continue
		}
		// cache stateName and attrFiles to be used for all CPUs
		if stateName == nil {
			stateName = make(map[int]string)
			var err error

			states, err := cs.fs.CpuidleStates(cpu)
			if err != nil {
				return err
			}
			for _, state := range states {
				name, err := cs.fs.CpuidleStateAttrRead(cpu, state, "name")
				if err != nil {
					return err
				}
				stateName[state] = name
				cs.maxState = max(cs.maxState, state)
			}
		}

		for state, cstateName := range stateName {
			if !allFilters(cpu, cstateName, -1, nil) {
				continue
			}
			cstate := NewCstate(cstateName, cpu, state)
			for _, attr := range attributes {
				if !allFilters(cpu, cstateName, attr.id, nil) {
					continue
				}
				value, err := cs.fs.CpuidleStateAttrRead(cpu, state, attr.name)
				if err != nil {
					return err
				}
				if !allFilters(cpu, cstateName, attr.id, &value) {
					continue
				}
				cstate.setAttr(attr.id, &value)
			}
			if cstate.AttrCount() == 0 {
				continue
			}
			cs.Add(cstate)
		}
	}
	return nil
}

// Add adds a Cstate instance to the collection.
func (cs *Cstates) Add(cstate *Cstate) {
	state := cstate.State()
	cs.cstates = append(cs.cstates, cstate)
	if state > cs.maxState {
		cs.maxState = state
	}
}

// Names returns a sorted list of unique C-state names in the collection.
// The order of names corresponds to the order of state numbers, e.g.
// ["POLL", "C1E", "C6", "C8", "C10"], where "POLL" is state0, "C1E" is state1, etc.
// Lists C-state names present in the collection, not all possible names.
func (cs *Cstates) Names() []string {
	names := []string{}
	for state := 0; state <= cs.maxState; state++ {
		for _, cstate := range cs.cstates {
			if cstate.state == state {
				names = append(names, cstate.name)
				break
			}
		}
	}
	return names
}

// Copy returns a new Cstates instance containing copies of the
// C-states. If filters are given, result includes only matching
// C-states with matching attributes and values, and C-states without
// attributes are omitted.
func (cs *Cstates) Copy(filters ...CstatesFilter) *Cstates {
	newCs := NewCstates()
	newCs.fs = cs.fs
	allFilters := FilterAll(filters...)
	for _, cstate := range cs.cstates {
		if !allFilters(cstate.cpu, cstate.name, -1, nil) {
			continue
		}
		newCstate := cstate.Copy(filters...)
		if newCstate.AttrCount() == 0 {
			continue
		}
		newCs.Add(newCstate)
	}
	return newCs
}

// Apply writes attributes back to sysfs.
func (cs *Cstates) Apply() error {
	for _, cstate := range cs.cstates {
		for id, val := range cstate.attrVal {
			if val == nil {
				continue
			}
			if attributes[id].readOnly {
				continue
			}
			if err := cs.fs.CpuidleStateAttrWrite(cstate.CPU(), cstate.State(), attrIDToName[AttrID(id)], *val); err != nil {
				return err
			}
		}
	}
	return nil
}

// CPUs returns a set of CPU IDs present in the collection
func (cs *Cstates) CPUs() utils.IDSet {
	cpus := utils.NewIDSet()
	for _, cstate := range cs.cstates {
		if cstate.AttrCount() > 0 {
			cpus.Add(cstate.CPU())
		}
	}
	return cpus
}

// Cstate returns the Cstate instance for the given CPU ID and C-state name,
// or nil if not found.
func (cs *Cstates) Cstate(cpu utils.ID, name string) *Cstate {
	for _, cstate := range cs.cstates {
		if cpu == cstate.CPU() && name == cstate.Name() {
			return cstate
		}
	}
	return nil
}

// Attrs returns a list of attribute IDs that have a value set.
func (cs *Cstates) Attrs() []AttrID {
	attrSet := [AttrIDCount]bool{}
	for _, cstate := range cs.cstates {
		for _, attr := range cstate.Attrs() {
			attrSet[attr] = true
		}
	}
	attrs := []AttrID{}
	for idx := range attrSet {
		if attrSet[idx] {
			attrs = append(attrs, AttrID(idx))
		}
	}
	return attrs
}

// GetAttr returns the attribute value for the given CPU ID and C-state name,
func (cs *Cstates) GetAttr(cpu utils.ID, name string, attr AttrID) *string {
	if c := cs.Cstate(cpu, name); c != nil {
		return c.GetAttr(attr)
	}
	return nil
}

// ClearAttrs clears the given attribute for all C-states in the collection.
func (cs *Cstates) ClearAttrs(attrs ...AttrID) {
	for _, cstate := range cs.cstates {
		for _, attr := range attrs {
			cstate.SetAttr(attr, nil)
		}
	}
}

// SetAttrs sets the given attribute to the given value for all C-states
// in the collection.
func (cs *Cstates) SetAttrs(attr AttrID, val string) {
	for _, cstate := range cs.cstates {
		cstate.SetAttr(attr, &val)
	}
}

// String returns a string representation of the Cstates instance.
func (cs *Cstates) String() string {
	// sort cs.cstates by CPU and state
	slices.SortFunc(cs.cstates, func(ci, cj *Cstate) int {
		if ci.CPU() == cj.CPU() {
			return ci.State() - cj.State()
		}
		return ci.CPU() - cj.CPU()
	})
	str := []string{}
	for _, cstate := range cs.cstates {
		str = append(str, cstate.String())
	}
	return fmt.Sprintf("Cstates{fs=%v [%v]}", cs.fs != nil, strings.Join(str, ", "))
}
