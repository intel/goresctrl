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
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// resctrlInfo contains information about the RDT support in the system
type resctrlInfo struct {
	resctrlPath      string
	resctrlMountOpts map[string]struct{}
	numClosids       uint64
	cat              map[cacheLevel]catInfoAll
	l3mon            l3MonInfo
	mb               mbInfo
}

type cacheLevel string

const (
	L2 cacheLevel = "L2"
	L3 cacheLevel = "L3"
)

type catInfoAll struct {
	cacheIds []uint64
	unified  catInfo
	code     catInfo
	data     catInfo
}

type catInfo struct {
	cbmMask       Bitmask
	minCbmBits    uint64
	shareableBits Bitmask
}

type l3MonInfo struct {
	numRmids    uint64
	monFeatures []string
}

type mbInfo struct {
	cacheIds      []uint64
	bandwidthGran uint64
	delayLinear   uint64
	minBandwidth  uint64
	mbpsEnabled   bool // true if MBA_MBps is enabled
}

var mountInfoPath string = "/proc/mounts"

// getInfo is a helper method for a "unified API" for getting L3 information
func (i catInfoAll) getInfo() catInfo {
	switch {
	case i.code.Supported():
		return i.code
	case i.data.Supported():
		return i.data
	}
	return i.unified
}

func (i catInfoAll) cbmMask() Bitmask {
	mask := i.getInfo().cbmMask
	if mask != 0 {
		return mask
	}
	return Bitmask(^uint64(0))
}

func (i catInfoAll) minCbmBits() uint64 {
	return i.getInfo().minCbmBits
}

func getRdtInfo() (*resctrlInfo, error) {
	var err error
	info := &resctrlInfo{cat: make(map[cacheLevel]catInfoAll)}

	info.resctrlPath, info.resctrlMountOpts, err = getResctrlMountInfo()
	if err != nil {
		return info, rdtError("failed to detect resctrl mount point: %v", err)
	}
	log.Info("detected resctrl filesystem at %q", info.resctrlPath)

	// Check that RDT is available
	infopath := filepath.Join(info.resctrlPath, "info")
	if _, err := os.Stat(infopath); err != nil {
		return info, rdtError("failed to read RDT info from %q: %v", infopath, err)
	}

	cat := catInfoAll{}
	subpath := filepath.Join(infopath, "L3")
	if _, err = os.Stat(subpath); err == nil {
		cat.unified, info.numClosids, err = getCatInfo(subpath)
		if err != nil {
			return info, rdtError("failed to get L3 info from %q: %v", subpath, err)
		}
	}

	subpath = filepath.Join(infopath, "L3CODE")
	if _, err = os.Stat(subpath); err == nil {
		cat.code, info.numClosids, err = getCatInfo(subpath)
		if err != nil {
			return info, rdtError("failed to get L3CODE info from %q: %v", subpath, err)
		}
	}

	subpath = filepath.Join(infopath, "L3DATA")
	if _, err = os.Stat(subpath); err == nil {
		cat.data, info.numClosids, err = getCatInfo(subpath)
		if err != nil {
			return info, rdtError("failed to get L3DATA info from %q: %v", subpath, err)
		}
	}

	if cat.getInfo().Supported() {
		cat.cacheIds, err = getCacheIds(info.resctrlPath, "L3")
		if err != nil {
			return info, rdtError("failed to get L3 CAT cache IDs: %v", err)
		}
	}
	info.cat[L3] = cat

	subpath = filepath.Join(infopath, "L3_MON")
	if _, err = os.Stat(subpath); err == nil {
		info.l3mon, err = getL3MonInfo(subpath)
		if err != nil {
			return info, rdtError("failed to get L3_MON info from %q: %v", subpath, err)
		}
	}

	subpath = filepath.Join(infopath, "MB")
	if _, err = os.Stat(subpath); err == nil {
		info.mb, info.numClosids, err = getMBInfo(subpath)
		if err != nil {
			return info, rdtError("failed to get MBA info from %q: %v", subpath, err)
		}

		info.mb.cacheIds, err = getCacheIds(info.resctrlPath, "MB")
		if err != nil {
			return info, rdtError("failed to get MBA cache IDs: %v", err)
		}
	}

	return info, nil
}

func getCatInfo(basepath string) (catInfo, uint64, error) {
	var err error
	var numClosids uint64
	info := catInfo{}

	info.cbmMask, err = readFileBitmask(filepath.Join(basepath, "cbm_mask"))
	if err != nil {
		return info, numClosids, err
	}
	info.minCbmBits, err = readFileUint64(filepath.Join(basepath, "min_cbm_bits"))
	if err != nil {
		return info, numClosids, err
	}
	info.shareableBits, err = readFileBitmask(filepath.Join(basepath, "shareable_bits"))
	if err != nil {
		return info, numClosids, err
	}
	numClosids, err = readFileUint64(filepath.Join(basepath, "num_closids"))
	if err != nil {
		return info, numClosids, err
	}

	return info, numClosids, nil
}

// Supported returns true if L3 cache allocation has is supported and enabled in the system
func (i catInfo) Supported() bool {
	return i.cbmMask != 0
}

func getL3MonInfo(basepath string) (l3MonInfo, error) {
	var err error
	info := l3MonInfo{}

	info.numRmids, err = readFileUint64(filepath.Join(basepath, "num_rmids"))
	if err != nil {
		return info, err
	}

	lines, err := readFileString(filepath.Join(basepath, "mon_features"))
	if err != nil {
		return info, err
	}
	info.monFeatures = strings.Split(lines, "\n")
	sort.Strings(info.monFeatures)

	return info, nil
}

// Supported returns true if L3 monitoring is supported and enabled in the system
func (i l3MonInfo) Supported() bool {
	return i.numRmids != 0 && len(i.monFeatures) > 0
}

func getMBInfo(basepath string) (mbInfo, uint64, error) {
	var err error
	var numClosids uint64
	info := mbInfo{}

	info.bandwidthGran, err = readFileUint64(filepath.Join(basepath, "bandwidth_gran"))
	if err != nil {
		return info, numClosids, err
	}
	info.delayLinear, err = readFileUint64(filepath.Join(basepath, "delay_linear"))
	if err != nil {
		return info, numClosids, err
	}
	info.minBandwidth, err = readFileUint64(filepath.Join(basepath, "min_bandwidth"))
	if err != nil {
		return info, numClosids, err
	}
	numClosids, err = readFileUint64(filepath.Join(basepath, "num_closids"))
	if err != nil {
		return info, numClosids, err
	}

	// Detect MBps mode directly from mount options as it's not visible in MB
	// info directory
	_, mountOpts, err := getResctrlMountInfo()
	if err != nil {
		return info, numClosids, fmt.Errorf("failed to get resctrl mount options: %v", err)
	}
	if _, ok := mountOpts["mba_MBps"]; ok {
		info.mbpsEnabled = true
	}

	return info, numClosids, nil
}

// Supported returns true if memory bandwidth allocation has is supported and enabled in the system
func (i mbInfo) Supported() bool {
	return i.minBandwidth != 0
}

func getCacheIds(basepath string, prefix string) ([]uint64, error) {
	var ids []uint64

	// Parse cache IDs from the root schemata
	data, err := readFileString(filepath.Join(basepath, "schemata"))
	if err != nil {
		return ids, rdtError("failed to read root schemata: %v", err)
	}

	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		lineSplit := strings.SplitN(trimmed, ":", 2)

		// Find line with L3 or MB schema
		if len(lineSplit) == 2 && strings.HasPrefix(lineSplit[0], prefix) {
			schema := strings.Split(lineSplit[1], ";")
			ids = make([]uint64, len(schema))

			// Get individual cache configurations from the schema
			for idx, definition := range schema {
				split := strings.Split(definition, "=")
				if len(split) != 2 {
					return ids, rdtError("looks like an invalid schema %q", trimmed)
				}
				ids[idx], err = strconv.ParseUint(split[0], 10, 64)
				if err != nil {
					return ids, rdtError("failed to parse cache id in %q: %v", trimmed, err)
				}
			}
			return ids, nil
		}
	}
	return ids, rdtError("no %s resources in root schemata", prefix)
}

func getResctrlMountInfo() (string, map[string]struct{}, error) {
	mountOptions := map[string]struct{}{}

	f, err := os.Open(mountInfoPath)
	if err != nil {
		return "", mountOptions, err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		split := strings.Split(s.Text(), " ")
		if len(split) > 3 && split[2] == "resctrl" {
			opts := strings.Split(split[3], ",")
			for _, opt := range opts {
				mountOptions[opt] = struct{}{}
			}
			return split[1], mountOptions, nil
		}
	}
	return "", mountOptions, rdtError("resctrl not found in " + mountInfoPath)
}

func readFileUint64(path string) (uint64, error) {
	data, err := readFileString(path)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(data, 10, 64)
}

func readFileBitmask(path string) (Bitmask, error) {
	data, err := readFileString(path)
	if err != nil {
		return 0, err
	}

	value, err := strconv.ParseUint(data, 16, 64)
	return Bitmask(value), err
}

func readFileString(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	return strings.TrimSpace(string(data)), err
}
