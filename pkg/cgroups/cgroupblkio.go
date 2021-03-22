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

package cgroups

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// cgroups blkio parameter filenames.
var blkioWeightFiles = []string{"blkio.bfq.weight", "blkio.weight"}
var blkioWeightDeviceFiles = []string{"blkio.bfq.weight_device", "blkio.weight_device"}
var blkioThrottleReadBpsFiles = []string{"blkio.throttle.read_bps_device"}
var blkioThrottleWriteBpsFiles = []string{"blkio.throttle.write_bps_device"}
var blkioThrottleReadIOPSFiles = []string{"blkio.throttle.read_iops_device"}
var blkioThrottleWriteIOPSFiles = []string{"blkio.throttle.write_iops_device"}

// OciBlockIOParameters contains OCI standard configuration of cgroups blkio parameters.
//
// Effects of Weight and Rate values in SetBlkioParameters():
// Value  |  Effect
// -------+-------------------------------------------------------------------
//    -1  |  Do not write to cgroups, value is missing
//     0  |  Write to cgroups, will remove the setting as specified in cgroups blkio interface
//  other |  Write to cgroups, sets the value
type OciBlockIOParameters struct {
	Weight                  int64
	WeightDevice            OciDeviceWeights
	ThrottleReadBpsDevice   OciDeviceRates
	ThrottleWriteBpsDevice  OciDeviceRates
	ThrottleReadIOPSDevice  OciDeviceRates
	ThrottleWriteIOPSDevice OciDeviceRates
}

// OciDeviceWeight contains values for
// - blkio.[io-scheduler].weight
type OciDeviceWeight struct {
	Major  int64
	Minor  int64
	Weight int64
}

// OciDeviceRate contains values for
// - blkio.throttle.read_bps_device
// - blkio.throttle.write_bps_device
// - blkio.throttle.read_iops_device
// - blkio.throttle.write_iops_device
type OciDeviceRate struct {
	Major int64
	Minor int64
	Rate  int64
}

// OciDeviceWeights contains weights for devices
type OciDeviceWeights []OciDeviceWeight

// OciDeviceRates contains throttling rates for devices
type OciDeviceRates []OciDeviceRate

// OciDeviceParameters interface provides functions common to OciDeviceWeights and OciDeviceRates
type OciDeviceParameters interface {
	Append(maj, min, val int64)
	Update(maj, min, val int64)
}

// Append appends (major, minor, value) to OciDeviceWeights slice.
func (w *OciDeviceWeights) Append(maj, min, val int64) {
	*w = append(*w, OciDeviceWeight{Major: maj, Minor: min, Weight: val})
}

// Append appends (major, minor, value) to OciDeviceRates slice.
func (r *OciDeviceRates) Append(maj, min, val int64) {
	*r = append(*r, OciDeviceRate{Major: maj, Minor: min, Rate: val})
}

// Update updates device weight in OciDeviceWeights slice, or appends it if not found.
func (w *OciDeviceWeights) Update(maj, min, val int64) {
	for index, devWeight := range *w {
		if devWeight.Major == maj && devWeight.Minor == min {
			(*w)[index].Weight = val
			return
		}
	}
	w.Append(maj, min, val)
}

// Update updates device rate in OciDeviceRates slice, or appends it if not found.
func (r *OciDeviceRates) Update(maj, min, val int64) {
	for index, devRate := range *r {
		if devRate.Major == maj && devRate.Minor == min {
			(*r)[index].Rate = val
			return
		}
	}
	r.Append(maj, min, val)
}

// NewOciBlockIOParameters creates new OciBlockIOParameters instance.
func NewOciBlockIOParameters() OciBlockIOParameters {
	return OciBlockIOParameters{
		Weight: -1,
	}
}

// NewOciDeviceWeight creates new OciDeviceWeight instance.
func NewOciDeviceWeight() OciDeviceWeight {
	return OciDeviceWeight{
		Major:  -1,
		Minor:  -1,
		Weight: -1,
	}
}

// NewOciDeviceRate creates new OciDeviceRate instance.
func NewOciDeviceRate() OciDeviceRate {
	return OciDeviceRate{
		Major: -1,
		Minor: -1,
		Rate:  -1,
	}
}

type devMajMin struct {
	Major int64
	Minor int64
}

// ResetBlkioParameters adds new, changes existing and removes missing blockIO parameters in cgroupsDir
func ResetBlkioParameters(groupDir string, blockIO OciBlockIOParameters) error {
	var errors *multierror.Error
	oldBlockIO, getErr := GetBlkioParameters(groupDir)
	errors = multierror.Append(errors, getErr)
	newBlockIO := NewOciBlockIOParameters()
	newBlockIO.Weight = blockIO.Weight
	newBlockIO.WeightDevice = resetDevWeights(oldBlockIO.WeightDevice, blockIO.WeightDevice)
	newBlockIO.ThrottleReadBpsDevice = resetDevRates(oldBlockIO.ThrottleReadBpsDevice, blockIO.ThrottleReadBpsDevice)
	newBlockIO.ThrottleWriteBpsDevice = resetDevRates(oldBlockIO.ThrottleWriteBpsDevice, blockIO.ThrottleWriteBpsDevice)
	newBlockIO.ThrottleReadIOPSDevice = resetDevRates(oldBlockIO.ThrottleReadIOPSDevice, blockIO.ThrottleReadIOPSDevice)
	newBlockIO.ThrottleWriteIOPSDevice = resetDevRates(oldBlockIO.ThrottleWriteIOPSDevice, blockIO.ThrottleWriteIOPSDevice)
	errors = multierror.Append(errors, SetBlkioParameters(groupDir, newBlockIO))
	return errors.ErrorOrNil()
}

// resetDevWeights adds wanted weight parameters to new and resets unwanted weights
func resetDevWeights(old, wanted []OciDeviceWeight) []OciDeviceWeight {
	new := []OciDeviceWeight{}
	seenDev := map[devMajMin]bool{}
	for _, wdp := range wanted {
		seenDev[devMajMin{wdp.Major, wdp.Minor}] = true
		new = append(new, wdp)
	}
	for _, wdp := range old {
		if !seenDev[devMajMin{wdp.Major, wdp.Minor}] {
			new = append(new, OciDeviceWeight{wdp.Major, wdp.Minor, 0})
		}
	}
	return new
}

// resetDevRates adds wanted rate parameters to new and resets unwanted rates
func resetDevRates(old, wanted []OciDeviceRate) []OciDeviceRate {
	new := []OciDeviceRate{}
	seenDev := map[devMajMin]bool{}
	for _, rdp := range wanted {
		new = append(new, rdp)
		seenDev[devMajMin{rdp.Major, rdp.Minor}] = true
	}
	for _, rdp := range old {
		if !seenDev[devMajMin{rdp.Major, rdp.Minor}] {
			new = append(new, OciDeviceRate{rdp.Major, rdp.Minor, 0})
		}
	}
	return new
}

// GetBlkioParameters returns OCI BlockIO parameters from files in cgroups blkio controller directory.
func GetBlkioParameters(group string) (OciBlockIOParameters, error) {
	var errors *multierror.Error
	blockIO := NewOciBlockIOParameters()

	errors = multierror.Append(errors, readWeight(group, blkioWeightFiles, &blockIO.Weight))
	errors = multierror.Append(errors, readOciDeviceParameters(group, blkioWeightDeviceFiles, &blockIO.WeightDevice))
	errors = multierror.Append(errors, readOciDeviceParameters(group, blkioThrottleReadBpsFiles, &blockIO.ThrottleReadBpsDevice))
	errors = multierror.Append(errors, readOciDeviceParameters(group, blkioThrottleWriteBpsFiles, &blockIO.ThrottleWriteBpsDevice))
	errors = multierror.Append(errors, readOciDeviceParameters(group, blkioThrottleReadIOPSFiles, &blockIO.ThrottleReadIOPSDevice))
	errors = multierror.Append(errors, readOciDeviceParameters(group, blkioThrottleWriteIOPSFiles, &blockIO.ThrottleWriteIOPSDevice))
	return blockIO, errors.ErrorOrNil()
}

// readWeight parses int64 from a cgroups entry
func readWeight(groupDir string, filenames []string, rv *int64) error {
	contents, err := readFirstFile(groupDir, filenames)
	if err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(strings.TrimSuffix(contents, "\n"), 10, 64)
	if err != nil {
		return fmt.Errorf("parsing weight from %#v found in %v failed: %w", contents, filenames, err)
	}
	*rv = parsed
	return nil
}

// readOciDeviceParameters parses device lines used for weights and throttling rates
func readOciDeviceParameters(groupDir string, filenames []string, params OciDeviceParameters) error {
	var errors *multierror.Error
	contents, err := readFirstFile(groupDir, filenames)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(contents, "\n") {
		// Device weight files may have "default NNN" line at the beginning. Skip it.
		if line == "" || strings.HasPrefix(line, "default ") {
			continue
		}
		// Expect syntax MAJOR:MINOR VALUE
		devVal := strings.Split(line, " ")
		if len(devVal) != 2 {
			errors = multierror.Append(errors, fmt.Errorf("invalid line %q, single space expected", line))
			continue
		}
		majMin := strings.Split(devVal[0], ":")
		if len(majMin) != 2 {
			errors = multierror.Append(errors, fmt.Errorf("invalid line %q, single colon expected before space", line))
			continue
		}
		major, majErr := strconv.ParseInt(majMin[0], 10, 64)
		minor, minErr := strconv.ParseInt(majMin[1], 10, 64)
		value, valErr := strconv.ParseInt(devVal[1], 10, 64)
		if majErr != nil || minErr != nil || valErr != nil {
			errors = multierror.Append(errors, fmt.Errorf("invalid number when parsing \"major:minor value\" from \"%s:%s %s\"", majMin[0], majMin[1], devVal[1]))
			continue
		}
		params.Append(major, minor, value)
	}
	return errors.ErrorOrNil()
}

// readFirstFile returns contents of the first successfully read entry.
func readFirstFile(groupDir string, filenames []string) (string, error) {
	var errors *multierror.Error
	// If reading all the files fails, return list of read errors.
	for _, filename := range filenames {
		content, err := Blkio.Group(groupDir).Read(filename)
		if err == nil {
			return content, nil
		}
		errors = multierror.Append(errors, err)
	}
	err := errors.ErrorOrNil()
	if err != nil {
		return "", fmt.Errorf("could not read any of files %q: %w", filenames, err)
	}
	return "", nil
}

// SetBlkioParameters writes OCI BlockIO parameters to files in cgroups blkio contoller directory.
func SetBlkioParameters(group string, blockIO OciBlockIOParameters) error {
	var errors *multierror.Error
	if blockIO.Weight >= 0 {
		errors = multierror.Append(errors, writeFirstFile(group, blkioWeightFiles, "%d", blockIO.Weight))
	}
	for _, wd := range blockIO.WeightDevice {
		errors = multierror.Append(errors, writeFirstFile(group, blkioWeightDeviceFiles, "%d:%d %d", wd.Major, wd.Minor, wd.Weight))
	}
	for _, rd := range blockIO.ThrottleReadBpsDevice {
		errors = multierror.Append(errors, writeFirstFile(group, blkioThrottleReadBpsFiles, "%d:%d %d", rd.Major, rd.Minor, rd.Rate))
	}
	for _, rd := range blockIO.ThrottleWriteBpsDevice {
		errors = multierror.Append(errors, writeFirstFile(group, blkioThrottleWriteBpsFiles, "%d:%d %d", rd.Major, rd.Minor, rd.Rate))
	}
	for _, rd := range blockIO.ThrottleReadIOPSDevice {
		errors = multierror.Append(errors, writeFirstFile(group, blkioThrottleReadIOPSFiles, "%d:%d %d", rd.Major, rd.Minor, rd.Rate))
	}
	for _, rd := range blockIO.ThrottleWriteIOPSDevice {
		errors = multierror.Append(errors, writeFirstFile(group, blkioThrottleWriteIOPSFiles, "%d:%d %d", rd.Major, rd.Minor, rd.Rate))
	}
	return errors.ErrorOrNil()
}

// writeFirstFile writes content to the first existing file in the list under groupDir.
func writeFirstFile(groupDir string, filenames []string, format string, args ...interface{}) error {
	var errors *multierror.Error
	// Returns list of errors from writes, list of single error due to all filenames missing or nil on success.
	for _, filename := range filenames {
		if err := Blkio.Group(groupDir).Write(filename, format, args...); err != nil {
			errors = multierror.Append(errors, err)
			continue
		}
		return nil
	}
	err := errors.ErrorOrNil()
	if err != nil {
		data := fmt.Sprintf(format, args...)
		return fmt.Errorf("writing all files %v failed, errors: %w, content %q", filenames, err, data)
	}
	return nil
}
