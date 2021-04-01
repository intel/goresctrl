/*
Copyright 2019 Intel Corporation

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

package rdt

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strconv"
	"strings"

	"github.com/intel/goresctrl/pkg/utils"
)

// Config represents the raw RDT configuration data from the configmap
type Config struct {
	Options    Options `json:"options"`
	Partitions map[string]struct {
		L2Allocation interface{} `json:"l2Allocation"`
		L3Allocation interface{} `json:"l3Allocation"`
		MBAllocation interface{} `json:"mbAllocation"`
		Classes      map[string]struct {
			L2Schema interface{} `json:"l2Schema"`
			L3Schema interface{} `json:"l3Schema"`
			MBSchema interface{} `json:"mbSchema"`
		} `json:"classes"`
	} `json:"partitions"`
}

// config represents the final (parsed and resolved) runtime configuration of
// RDT Control
type config struct {
	Options    Options
	Partitions partitionSet
	Classes    classSet
}

// partitionSet represents the pool of rdt partitions
type partitionSet map[string]*partitionConfig

// classSet represents the pool of rdt classes
type classSet map[string]*classConfig

// partitionConfig is the final configuration of one partition
type partitionConfig struct {
	CAT map[cacheLevel]catSchema
	MB  mbSchema
}

// classConfig represents configuration of one class, i.e. one CTRL group in
// the Linux resctrl interface
type classConfig struct {
	Partition string
	CATSchema map[cacheLevel]catSchema
	MBSchema  mbSchema
}

// Options contains the common settings for all classes
type Options struct {
	L2 catOptions `json:"l2"`
	L3 catOptions `json:"l3"`
	MB mbOptions  `json:"mb"`
}

// catOptions contains the common settings for cache allocation
type catOptions struct {
	Optional bool
}

// mbOptions contains the common settings for memory bandwidth allocation
type mbOptions struct {
	Optional bool
}

// catSchema represents a cache part of the schemata of a class (i.e. resctrl group)
type catSchema struct {
	Lvl   cacheLevel
	Alloc catSchemaRaw
}

// catSchemaRaw is the cache schemata without the information about cache level
type catSchemaRaw map[uint64]catAllocation

// mbSchema represents the MB part of the schemata of a class (i.e. resctrl group)
type mbSchema map[uint64]uint64

// catAllocation describes the allocation configuration for one cache id
type catAllocation struct {
	Unified cacheAllocation
	Code    cacheAllocation `json:",omitempty"`
	Data    cacheAllocation `json:",omitempty"`
}

// cacheAllocation is the basic interface for handling cache allocations of one
// type (unified, code, data)
type cacheAllocation interface {
	Overlay(Bitmask, uint64) (Bitmask, error)
}

// catAbsoluteAllocation represents an explicitly specified cache allocation
// bitmask
type catAbsoluteAllocation Bitmask

// catPctAllocation represents a relative (percentage) share of the available
// bitmask
type catPctAllocation uint64

// catPctRangeAllocation represents a percentage range of the available bitmask
type catPctRangeAllocation struct {
	lowPct  uint64
	highPct uint64
}

// catSchemaType represents different L3 cache allocation schemes
type catSchemaType string

const (
	// catSchemaTypeUnified is the schema type when CDP is not enabled
	catSchemaTypeUnified catSchemaType = "unified"
	// catSchemaTypeCode is the 'code' part of CDP schema
	catSchemaTypeCode catSchemaType = "code"
	// catSchemaTypeData is the 'data' part of CDP schema
	catSchemaTypeData catSchemaType = "data"
)

func (o Options) Cat(lvl cacheLevel) catOptions {
	switch lvl {
	case L2:
		return o.L2
	case L3:
		return o.L3
	}
	return catOptions{}
}

func (t catSchemaType) ToResctrlStr() string {
	if t == catSchemaTypeUnified {
		return ""
	}
	return strings.ToUpper(string(t))
}

const (
	mbSuffixPct  = "%"
	mbSuffixMbps = "MBps"
)

func newCatSchema(typ cacheLevel) catSchema {
	return catSchema{
		Lvl:   typ,
		Alloc: make(map[uint64]catAllocation),
	}
}

// ToStr returns the CAT schema in a format accepted by the Linux kernel
// resctrl (schemata) interface
func (s catSchema) ToStr(typ catSchemaType, baseSchema catSchema) (string, error) {
	schema := string(s.Lvl) + typ.ToResctrlStr() + ":"
	sep := ""

	// Get a sorted slice of cache ids for deterministic output
	ids := make([]uint64, 0, len(baseSchema.Alloc))
	for id := range baseSchema.Alloc {
		ids = append(ids, id)
	}
	utils.SortUint64s(ids)

	minBits := info.cat[s.Lvl].minCbmBits()
	for _, id := range ids {
		baseMask, ok := baseSchema.Alloc[id].getEffective(typ).(catAbsoluteAllocation)
		if !ok {
			return "", fmt.Errorf("BUG: basemask not of type catAbsoluteAllocation")
		}
		bitmask := Bitmask(baseMask)

		if s.Alloc != nil {
			var err error

			masks := s.Alloc[id]
			overlayMask := masks.getEffective(typ)

			bitmask, err = overlayMask.Overlay(bitmask, minBits)
			if err != nil {
				return "", err
			}
		}
		schema += fmt.Sprintf("%s%d=%x", sep, id, bitmask)
		sep = ";"
	}

	return schema + "\n", nil
}

func (a catAllocation) get(typ catSchemaType) cacheAllocation {
	switch typ {
	case catSchemaTypeCode:
		return a.Code
	case catSchemaTypeData:
		return a.Data
	}
	return a.Unified
}

func (a catAllocation) set(typ catSchemaType, v cacheAllocation) catAllocation {
	switch typ {
	case catSchemaTypeCode:
		a.Code = v
	case catSchemaTypeData:
		a.Data = v
	default:
		a.Unified = v
	}

	return a
}

func (a catAllocation) getEffective(typ catSchemaType) cacheAllocation {
	switch typ {
	case catSchemaTypeCode:
		if a.Code != nil {
			return a.Code
		}
	case catSchemaTypeData:
		if a.Data != nil {
			return a.Data
		}
	}
	// Use Unified as the default/fallback for Code and Data
	return a.Unified
}

// Overlay function of the cacheAllocation interface
func (a catAbsoluteAllocation) Overlay(baseMask Bitmask, minBits uint64) (Bitmask, error) {
	if err := verifyCatBaseMask(baseMask, minBits); err != nil {
		return 0, err
	}

	shiftWidth := baseMask.lsbOne()

	// Treat our bitmask relative to the basemask
	bitmask := Bitmask(a) << shiftWidth

	// Do bounds checking that we're "inside" the base mask
	if bitmask|baseMask != baseMask {
		return 0, rdtError("bitmask %#x (%#x << %d) does not fit basemask %#x", bitmask, a, shiftWidth, baseMask)
	}

	return bitmask, nil
}

// MarshalJSON implements the Marshaler interface of "encoding/json"
func (a catAbsoluteAllocation) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%#x\"", a)), nil
}

// Overlay function of the cacheAllocation interface
func (a catPctAllocation) Overlay(baseMask Bitmask, minBits uint64) (Bitmask, error) {
	return catPctRangeAllocation{highPct: uint64(a)}.Overlay(baseMask, minBits)
}

// Overlay function of the cacheAllocation interface
func (a catPctRangeAllocation) Overlay(baseMask Bitmask, minBits uint64) (Bitmask, error) {
	if err := verifyCatBaseMask(baseMask, minBits); err != nil {
		return 0, err
	}

	baseMaskMsb := uint64(baseMask.msbOne())
	baseMaskLsb := uint64(baseMask.lsbOne())
	baseMaskNumBits := baseMaskMsb - baseMaskLsb + 1

	low, high := a.lowPct, a.highPct
	if low == 0 {
		low = 1
	}
	if low > high || low > 100 || high > 100 {
		return 0, rdtError("invalid percentage range in %v", a)
	}

	// Convert percentage limits to bit numbers
	// Our effective range is 1%-100%, use substraction (-1) because of
	// arithmetics, so that we don't overflow on 100%
	lsb := (low - 1) * baseMaskNumBits / 100
	msb := (high - 1) * baseMaskNumBits / 100

	// Make sure the number of bits set satisfies the minimum requirement
	numBits := msb - lsb + 1
	if numBits < minBits {
		gap := minBits - numBits

		// First, widen the mask from the "lsb end"
		if gap <= lsb {
			lsb -= gap
			gap = 0
		} else {
			gap -= lsb
			lsb = 0
		}
		// If needed, widen the mask from the "msb end"
		msbAvailable := baseMaskNumBits - msb - 1
		if gap <= msbAvailable {
			msb += gap
		} else {
			return 0, rdtError("BUG: not enough bits available for cache bitmask (%s applied on basemask %#x)", a, baseMask)
		}
	}

	value := ((1 << (msb - lsb + 1)) - 1) << (lsb + baseMaskLsb)

	return Bitmask(value), nil
}

func verifyCatBaseMask(baseMask Bitmask, minBits uint64) error {
	if baseMask == 0 {
		return fmt.Errorf("empty basemask not allowed")
	}

	// Check that the basemask contains one (and only one) contiguous block of
	// (enough) bits set
	baseMaskWidth := baseMask.msbOne() - baseMask.lsbOne() + 1
	if bits.OnesCount64(uint64(baseMask)) != baseMaskWidth {
		return fmt.Errorf("invalid basemask %#x: more than one block of bits set", baseMask)
	}
	if uint64(bits.OnesCount64(uint64(baseMask))) < minBits {
		return fmt.Errorf("invalid basemask %#x: fewer than %d bits set", baseMask, minBits)
	}

	return nil
}

// MarshalJSON implements the Marshaler interface of "encoding/json"
func (a catPctAllocation) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%d%%\"", a)), nil
}

// MarshalJSON implements the Marshaler interface of "encoding/json"
func (a catPctRangeAllocation) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%d-%d%%\"", a.lowPct, a.highPct)), nil
}

// ToStr returns the MB schema in a format accepted by the Linux kernel
// resctrl (schemata) interface
func (s mbSchema) ToStr(base map[uint64]uint64) string {
	schema := "MB:"
	sep := ""

	// Get a sorted slice of cache ids for deterministic output
	ids := make([]uint64, 0, len(base))
	for id := range base {
		ids = append(ids, id)
	}
	utils.SortUint64s(ids)

	for _, id := range ids {
		baseAllocation := base[id]
		value := uint64(0)
		if info.mb.mbpsEnabled {
			value = math.MaxUint32
			if s != nil {
				value = s[id]
			}
			// Limit to given base value
			if value > baseAllocation {
				value = baseAllocation
			}
		} else {
			allocation := uint64(100)
			if s != nil {
				allocation = s[id]
			}
			value = allocation * baseAllocation / 100
			// Guarantee minimum bw so that writing out the schemata does not fail
			if value < info.mb.minBandwidth {
				value = info.mb.minBandwidth
			}
		}

		schema += fmt.Sprintf("%s%d=%d", sep, id, value)
		sep = ";"
	}

	return schema + "\n"
}

// listStrToArray parses a string containing a human-readable list of numbers
// into an integer array
func listStrToArray(str string) ([]int, error) {
	a := []int{}

	// Empty list
	if len(str) == 0 {
		return a, nil
	}

	ranges := strings.Split(str, ",")
	for _, ran := range ranges {
		split := strings.SplitN(ran, "-", 2)

		// We limit to 8 bits in order to avoid accidental super long slices
		num, err := strconv.ParseInt(split[0], 10, 8)
		if err != nil {
			return a, rdtError("invalid integer %q: %v", str, err)
		}

		if len(split) == 1 {
			a = append(a, int(num))
		} else {
			endNum, err := strconv.ParseInt(split[1], 10, 8)
			if err != nil {
				return a, rdtError("invalid integer in range %q: %v", str, err)
			}
			if endNum <= num {
				return a, rdtError("invalid integer range %q in %q", ran, str)
			}
			for i := num; i <= endNum; i++ {
				a = append(a, int(i))
			}
		}
	}
	sort.Ints(a)
	return a, nil
}

// resolve tries to resolve the requested configuration into a working
// configuration
func (c *Config) resolve() (config, error) {
	var err error
	conf := config{Options: c.Options}

	log.DebugBlock("", "resolving configuration: |\n%s", utils.DumpJSON(c))

	conf.Partitions, err = c.resolvePartitions()
	if err != nil {
		return conf, err
	}

	conf.Classes, err = c.resolveClasses()
	if err != nil {
		return conf, err
	}

	return conf, nil
}

// resolvePartitions tries to resolve the requested resource allocations of
// partitions
func (c *Config) resolvePartitions() (partitionSet, error) {
	// Initialize empty partition configuration
	conf := make(partitionSet, len(c.Partitions))
	for name := range c.Partitions {
		conf[name] = &partitionConfig{
			CAT: map[cacheLevel]catSchema{
				L2: newCatSchema(L2),
				L3: newCatSchema(L3),
			},
			MB: make(mbSchema, len(info.mb.cacheIds))}
	}

	// Resolve L2 partition allocations
	err := c.resolveCatPartitions(L2, conf)
	if err != nil {
		return nil, err
	}

	// Try to resolve L3 partition allocations
	err = c.resolveCatPartitions(L3, conf)
	if err != nil {
		return nil, err
	}

	// Try to resolve MB partition allocations
	err = c.resolveMBPartitions(conf)
	if err != nil {
		return nil, err
	}

	return conf, nil
}

// resolveCatPartitions tries to resolve requested cache allocations between partitions
func (c *Config) resolveCatPartitions(lvl cacheLevel, conf partitionSet) error {
	// Resolve partitions in sorted order for reproducibility
	names := make([]string, 0, len(c.Partitions))
	for name := range c.Partitions {
		names = append(names, name)
	}
	sort.Strings(names)

	parser := newCatConfigParser(lvl)
	resolver := newCacheResolver(lvl, names)

	// Parse requested allocations from raw config load the resolver
	for _, name := range names {
		var allocations catSchema
		var err error
		switch lvl {
		case L2:
			allocations, err = parser.parse(c.Partitions[name].L2Allocation)
		case L3:
			allocations, err = parser.parse(c.Partitions[name].L3Allocation)
		}
		if err != nil {
			return fmt.Errorf("failed to parse %s allocation request for partition %q: %v", lvl, name, err)
		}

		resolver.requests[name] = allocations.Alloc
	}

	// Run resolver fo partition allocations
	grants, err := resolver.resolve()
	if err != nil {
		return err
	}
	if grants == nil {
		log.Debug("%s allocation disabled for all partitions", lvl)
		return nil
	}

	for name, grant := range grants {
		conf[name].CAT[lvl] = grant
	}

	log.Info("actual (and requested) %s allocations per partition and cache id:", lvl)
	infoStr := ""
	for name, partition := range resolver.requests {
		infoStr += "\n    " + name
		for _, id := range resolver.ids {
			infoStr += fmt.Sprintf("\n      %2d: ", id)
			allocationReq := partition[id]
			for _, typ := range []catSchemaType{catSchemaTypeUnified, catSchemaTypeCode, catSchemaTypeData} {
				infoStr += string(typ) + " "
				requested := allocationReq.get(typ)
				switch v := requested.(type) {
				case catAbsoluteAllocation:
					infoStr += fmt.Sprintf("<absolute %#x>  ", v)
				case catPctAllocation:
					granted := grants[name].Alloc[id].get(typ).(catAbsoluteAllocation)
					requestedPct := fmt.Sprintf("(%d%%)", v)
					truePct := float64(bits.OnesCount64(uint64(granted))) * 100 / float64(resolver.bitsTotal)
					infoStr += fmt.Sprintf("%5.1f%% %-6s ", truePct, requestedPct)
				case nil:
					infoStr += "<not specified>  "
				}
			}
		}
		infoStr += "\n"
	}
	log.InfoBlock("", "%s", infoStr)

	return nil
}

// cacheResolver is a helper for resolving exclusive (partition) cache // allocation requests
type cacheResolver struct {
	lvl        cacheLevel
	ids        []uint64
	minBits    uint64
	bitsTotal  uint64
	partitions []string
	requests   map[string]catSchemaRaw
	grants     map[string]catSchema
}

func newCacheResolver(lvl cacheLevel, partitions []string) *cacheResolver {
	r := &cacheResolver{
		lvl:        lvl,
		ids:        info.cat[lvl].cacheIds,
		minBits:    info.cat[lvl].minCbmBits(),
		bitsTotal:  uint64(info.cat[lvl].cbmMask().lsbZero()),
		partitions: partitions,
		requests:   make(map[string]catSchemaRaw, len(partitions)),
		grants:     make(map[string]catSchema, len(partitions))}

	for _, p := range partitions {
		r.grants[p] = catSchema{Lvl: lvl, Alloc: make(catSchemaRaw, len(r.ids))}
	}

	return r
}

func (r *cacheResolver) resolve() (map[string]catSchema, error) {
	for _, id := range r.ids {
		err := r.resolveID(id)
		if err != nil {
			return nil, err
		}
	}
	return r.grants, nil
}

// resolveCacheID resolves the partition allocations for one cache id
func (r *cacheResolver) resolveID(id uint64) error {
	for _, typ := range []catSchemaType{catSchemaTypeUnified, catSchemaTypeCode, catSchemaTypeData} {
		log.Debug("resolving partitions for %q schema for cache id %d", typ, id)
		err := r.resolveType(id, typ)
		if err != nil {
			return err
		}
	}
	return nil
}

// resolveType resolve one schema type for one cache id
func (r *cacheResolver) resolveType(id uint64, typ catSchemaType) error {
	// Sanity check: if any partition has l3 allocation of this schema type
	// configured check that all other partitions have it, too
	a := r.requests[r.partitions[0]][id].get(typ)
	isNil := a == nil
	for _, partition := range r.partitions {
		if (r.requests[partition][id].get(typ) == nil) != isNil {
			return fmt.Errorf("partition %q missing %s %q allocation request for cache id %d", partition, r.lvl, typ, id)
		}
	}

	// Act depending on the type of the first request in the list
	switch a.(type) {
	case catAbsoluteAllocation:
		return r.resolveAbsolute(id, typ)
	case nil:
	default:
		return r.resolveRelative(id, typ)
	}
	return nil
}

func (r *cacheResolver) resolveRelative(id uint64, typ catSchemaType) error {
	type reqHelper struct {
		name string
		req  uint64
	}

	// Sanity check:
	// 1. allocation requests are of the same type (relative)
	// 2. total allocation requested for this cache id does not exceed 100 percent
	// Additionally fill a helper structure for sorting partitions
	percentageTotal := uint64(0)
	reqs := make([]reqHelper, 0, len(r.partitions))
	for _, partition := range r.partitions {
		switch a := r.requests[partition][id].get(typ).(type) {
		case catPctAllocation:
			percentageTotal += uint64(a)
			reqs = append(reqs, reqHelper{name: partition, req: uint64(a)})
		case catAbsoluteAllocation:
			return fmt.Errorf("error resolving %s allocation for cache id %d: mixing "+
				"relative and absolute allocations between partitions not supported", r.lvl, id)
		case catPctRangeAllocation:
			return fmt.Errorf("percentage ranges in partition allocation not supported")
		default:
			return fmt.Errorf("BUG: unknown cacheAllocation type %T", a)
		}
	}
	if percentageTotal < 100 {
		log.Info("requested total %s %q partition allocation for cache id %d <100%% (%d%%)", r.lvl, typ, id, percentageTotal)
	} else if percentageTotal > 100 {
		return fmt.Errorf("accumulated %s %q partition allocation requests for cache id %d exceeds 100%% (%d%%)", r.lvl, typ, id, percentageTotal)
	}

	// Sort partition allocations. We want to resolve smallest allocations
	// first in order to try to ensure that all allocations can be satisfied
	// because small percentages might need to be rounded up
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].req < reqs[j].req
	})

	// Calculate number of bits granted to each partition.
	grants := make(map[string]uint64, len(r.partitions))
	bitsTotal := percentageTotal * uint64(r.bitsTotal) / 100
	bitsAvailable := bitsTotal
	for i, req := range reqs {
		percentageAvailable := bitsAvailable * percentageTotal / bitsTotal

		// This might happen e.g. if number of partitions would be greater
		// than the total number of bits
		if bitsAvailable < r.minBits {
			return fmt.Errorf("unable to resolve %s allocation for cache id %d, not enough exlusive bits available", r.lvl, id)
		}

		// Use integer arithmetics, effectively always rounding down
		// fractional allocations i.e. trying to avoid over-allocation
		numBits := req.req * bitsAvailable / percentageAvailable

		// Guarantee a non-zero allocation
		if numBits < r.minBits {
			numBits = r.minBits
		}
		// Don't overflow, allocate all remaining bits to the last partition
		if numBits > bitsAvailable || i == len(reqs)-1 {
			numBits = bitsAvailable
		}

		grants[req.name] = numBits
		bitsAvailable -= numBits
	}

	// Construct the actual bitmasks for each partition
	lsbID := uint64(0)
	for _, partition := range r.partitions {
		// Compose the actual bitmask
		v := r.grants[partition].Alloc[id].set(typ, catAbsoluteAllocation(Bitmask(((1<<grants[partition])-1)<<lsbID)))
		r.grants[partition].Alloc[id] = v

		lsbID += grants[partition]
	}

	return nil
}

func (r *cacheResolver) resolveAbsolute(id uint64, typ catSchemaType) error {
	// Just sanity check:
	// 1. allocation requests of the correct type (absolute)
	// 2. allocations do not overlap
	mask := Bitmask(0)
	for _, partition := range r.partitions {
		a, ok := r.requests[partition][id].get(typ).(catAbsoluteAllocation)
		if !ok {
			return fmt.Errorf("error resolving %s allocation for cache id %d: mixing absolute and relative allocations between partitions not supported", r.lvl, id)
		}
		if Bitmask(a)&mask > 0 {
			return fmt.Errorf("overlapping %s partition allocation requests for cache id %d", r.lvl, id)
		}
		mask |= Bitmask(a)

		r.grants[partition].Alloc[id] = r.grants[partition].Alloc[id].set(typ, a)
	}

	return nil
}

// resolveMBPartitions tries to resolve requested MB allocations between partitions
func (c *Config) resolveMBPartitions(conf partitionSet) error {
	// We use percentage values directly from the raw conf
	for name, partition := range c.Partitions {
		allocations, err := parseRawMBAllocations(partition.MBAllocation)
		if err != nil {
			return fmt.Errorf("failed to resolve MB allocation for partition %q: %v", name, err)
		}
		for id, allocation := range allocations {
			conf[name].MB[id] = allocation
			// Check that we don't go under the minimum allowed bandwidth setting
			if !info.mb.mbpsEnabled && allocation < info.mb.minBandwidth {
				conf[name].MB[id] = info.mb.minBandwidth
			}
		}
	}

	return nil
}

// resolveClasses tries to resolve class allocations of all partitions
func (c *Config) resolveClasses() (classSet, error) {
	classes := make(classSet)

	catL3Parser := newCatConfigParser(L3)
	catL2Parser := newCatConfigParser(L2)
	for bname, partition := range c.Partitions {
		for gname, class := range partition.Classes {
			if _, ok := classes[gname]; ok {
				return classes, fmt.Errorf("class names must be unique, %q defined multiple times", gname)
			}

			var err error
			gc := &classConfig{Partition: bname,
				CATSchema: make(map[cacheLevel]catSchema)}

			gc.CATSchema[L2], err = catL2Parser.parse(class.L2Schema)
			if err != nil {
				return classes, fmt.Errorf("failed to resolve L2 allocation for class %q: %v", gname, err)
			}
			if gc.CATSchema[L2].Alloc != nil && partition.L2Allocation == nil {
				return classes, fmt.Errorf("L2 allocation missing from partition %q but class %q specifies L2 schema", bname, gname)
			}

			gc.CATSchema[L3], err = catL3Parser.parse(class.L3Schema)
			if err != nil {
				return classes, fmt.Errorf("failed to resolve L3 allocation for class %q: %v", gname, err)
			}
			if gc.CATSchema[L3].Alloc != nil && partition.L3Allocation == nil {
				return classes, fmt.Errorf("L3 allocation missing from partition %q but class %q specifies L3 schema", bname, gname)
			}

			gc.MBSchema, err = parseRawMBAllocations(class.MBSchema)
			if err != nil {
				return classes, fmt.Errorf("failed to resolve MB allocation for class %q: %v", gname, err)
			}
			if gc.MBSchema != nil && partition.MBAllocation == nil {
				return classes, fmt.Errorf("MB allocation missing from partition %q but class %q specifies MB schema", bname, gname)
			}

			classes[gname] = gc
		}
	}

	return classes, nil
}

// parseRawMBAllocations parses a raw MB allocation
func parseRawMBAllocations(raw interface{}) (mbSchema, error) {
	rawValues, err := preparseRawAllocations(raw, []interface{}{}, info.mb.cacheIds)
	if err != nil || rawValues == nil {
		return nil, err
	}

	allocations := make(mbSchema, len(rawValues))
	for id, rawVal := range rawValues {
		strList, ok := rawVal.([]interface{})
		if !ok {
			return nil, fmt.Errorf("not a list value %q", rawVal)
		}
		allocations[id], err = parseMBAllocation(strList)
		if err != nil {
			return nil, err
		}
	}

	return allocations, nil
}

// preparseRawAllocations "pre-parses" the rawAllocations per each cache id. I.e. it assigns
// a raw (string) allocation for each cache id
func preparseRawAllocations(raw interface{}, defaultVal interface{}, cacheIds []uint64) (map[uint64]interface{}, error) {
	if raw == nil {
		return nil, nil
	}

	var rawPerCacheId map[string]interface{}
	allocations := make(map[uint64]interface{}, len(cacheIds))

	switch value := raw.(type) {
	case string:
		defaultVal = value
	case []interface{}:
		defaultVal = value
	case map[string]interface{}:
		if all, ok := value["all"]; ok {
			defaultVal = all
		} else if defaultVal == nil {
			return nil, fmt.Errorf("'all' is missing")
		}
		rawPerCacheId = value
	default:
		return allocations, fmt.Errorf("invalid structure of allocation schema '%v' (%T)", raw, raw)
	}

	for _, i := range cacheIds {
		allocations[i] = defaultVal
	}

	for key, val := range rawPerCacheId {
		if key == "all" {
			continue
		}
		ids, err := listStrToArray(key)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := allocations[uint64(id)]; ok {
				allocations[uint64(id)] = val
			}
		}
	}

	return allocations, nil
}

// catConfigParser is a helper for parsing cache allocation from the input config
type catConfigParser struct {
	lvl     cacheLevel
	ids     []uint64
	minBits uint64
}

func newCatConfigParser(lvl cacheLevel) *catConfigParser {
	return &catConfigParser{
		lvl:     lvl,
		ids:     info.cat[lvl].cacheIds,
		minBits: info.cat[lvl].minCbmBits()}
}

// parse parses an L3 cache allocation from the input config
func (p *catConfigParser) parse(raw interface{}) (catSchema, error) {
	rawValues, err := preparseRawAllocations(raw, "100%", p.ids)
	if err != nil || rawValues == nil {
		return catSchema{Lvl: p.lvl}, err
	}

	allocations := newCatSchema(p.lvl)
	for id, rawVal := range rawValues {
		allocations.Alloc[id], err = p.parseSchema(rawVal)
		if err != nil {
			return allocations, err
		}
	}

	return allocations, nil
}

// parseSchema parses a generic string or map of strings into l3Allocation struct
func (p *catConfigParser) parseSchema(raw interface{}) (catAllocation, error) {
	var err error
	allocation := catAllocation{}

	switch value := raw.(type) {
	case string:
		allocation.Unified, err = p.parseString(value)
		if err != nil {
			return allocation, err
		}
	case map[string]interface{}:
		for k, v := range value {
			s, ok := v.(string)
			if !ok {
				return allocation, fmt.Errorf("not a string value %q", v)
			}
			switch strings.ToLower(k) {
			case string(catSchemaTypeUnified):
				allocation.Unified, err = p.parseString(s)
			case string(catSchemaTypeCode):
				allocation.Code, err = p.parseString(s)
			case string(catSchemaTypeData):
				allocation.Data, err = p.parseString(s)
			}
			if err != nil {
				return allocation, err
			}
		}
	default:
		return allocation, fmt.Errorf("invalid structure of cache schema %q", raw)
	}

	// Sanity check for the configuration
	if allocation.Unified == nil {
		return allocation, fmt.Errorf("'unified' not specified in cache schema %s", raw)
	}
	if allocation.Code != nil && allocation.Data == nil {
		return allocation, fmt.Errorf("'code' specified but missing 'data' from cache schema %s", raw)
	}
	if allocation.Code == nil && allocation.Data != nil {
		return allocation, fmt.Errorf("'data' specified but missing 'code' from cache schema %s", raw)
	}

	return allocation, nil
}

// parseString parses a string value into cacheAllocation type
func (p *catConfigParser) parseString(data string) (cacheAllocation, error) {
	if data[len(data)-1] == '%' {
		// Percentages of the max number of bits
		split := strings.SplitN(data[0:len(data)-1], "-", 2)
		var allocation cacheAllocation

		if len(split) == 1 {
			pct, err := strconv.ParseUint(split[0], 10, 7)
			if err != nil {
				return allocation, err
			}
			if pct > 100 {
				return allocation, fmt.Errorf("invalid percentage value %q", data)
			}
			allocation = catPctAllocation(pct)
		} else {
			low, err := strconv.ParseUint(split[0], 10, 7)
			if err != nil {
				return allocation, err
			}
			high, err := strconv.ParseUint(split[1], 10, 7)
			if err != nil {
				return allocation, err
			}
			if low > high || low > 100 || high > 100 {
				return allocation, fmt.Errorf("invalid percentage range %q", data)
			}
			allocation = catPctRangeAllocation{lowPct: low, highPct: high}
		}

		return allocation, nil
	}

	// Absolute allocation
	var value uint64
	var err error
	if strings.HasPrefix(data, "0x") {
		// Hex value
		value, err = strconv.ParseUint(data[2:], 16, 64)
		if err != nil {
			return nil, err
		}
	} else {
		// Last, try "list" format (i.e. smthg like 0,2,5-9,...)
		tmp, err := ListStrToBitmask(data)
		value = uint64(tmp)
		if err != nil {
			return nil, err
		}
	}

	// Sanity check of absolute allocation: bitmask must (only) contain one
	// contiguous block of ones wide enough
	numOnes := bits.OnesCount64(value)
	if numOnes != 64-bits.LeadingZeros64(value)-bits.TrailingZeros64(value) {
		return nil, fmt.Errorf("invalid cache bitmask %q: more than one continuous block of ones", data)
	}
	if uint64(numOnes) < p.minBits {
		return nil, fmt.Errorf("invalid %s cache bitmask %q: number of bits less than %d", p.lvl, data, p.minBits)
	}

	return catAbsoluteAllocation(value), nil
}

// parseMBAllocation parses a generic string map into MB allocation value
func parseMBAllocation(raw []interface{}) (uint64, error) {
	for _, v := range raw {
		strVal, ok := v.(string)
		if !ok {
			log.Warn("ignoring non-string (%T) MBA allocation %v", v, v)
			continue
		}
		if strings.HasSuffix(strVal, mbSuffixPct) {
			if !info.mb.mbpsEnabled {
				value, err := strconv.ParseUint(strings.TrimSuffix(strVal, mbSuffixPct), 10, 7)
				if err != nil {
					return 0, err
				}
				return value, nil
			}
		} else if strings.HasSuffix(strVal, mbSuffixMbps) {
			if info.mb.mbpsEnabled {
				value, err := strconv.ParseUint(strings.TrimSuffix(strVal, mbSuffixMbps), 10, 32)
				if err != nil {
					return 0, err
				}
				return value, nil
			}
		} else {
			log.Warn("unrecognized MBA allocation unit in %q", strVal)
		}
	}

	// No value for the active mode was specified
	if info.mb.mbpsEnabled {
		return 0, fmt.Errorf("missing 'MBps' value from mbSchema; required because 'mba_MBps' is enabled in the system")
	}
	return 0, fmt.Errorf("missing '%%' value from mbSchema; required because percentage-based MBA allocation is enabled in the system")
}
