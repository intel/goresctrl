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

package sst

import (
	"flag"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/maps"
)

var enable = flag.Bool("sst.system-fuzz", false, "Enable SST fuzz tests that manipulate the system state (caution: requires root, manipulates configuration of the target system)")

// FuzzClosSetup is a fuzz test for the ClosSetup function. Caution needs to be
// taken when this test is run. The test needs to be run as root on an
// SST-enabled system. Moreover the test manipulates the actual SST
// configuration of the target system so a dedicated non-production system
// should be used. Test cannot be parallelized (as it depends on the system
// state). Because of these precautions the test needs to be explicitly
// enabled with "-sst.system-fuzz" flag.
//
// An example how to run the test:
//
//	# go test  -parallel=1 -fuzz=FuzzClosSetup  ./pkg/sst/... -sst.system-fuzz
func FuzzClosSetup(f *testing.F) {
	if !*enable {
		f.Skipf("skipping as sst.fuzz is not enabled")
	}

	pkgs, err := GetPackageInfo()
	if err != nil {
		f.Errorf("unable to get SST package info: %v", err)
	}
	pkgIDs := maps.Keys(pkgs)

	f.Add(pkgIDs[0], 0,
		0, 0, 0, 0, 0)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		2, 5, 100, 200, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 3,
		15, 15, 255, 255, 255)
	// Failure test cases
	f.Add(pkgIDs[len(pkgIDs)-1], 4,
		2, 5, 100, 200, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		20, 5, 100, 200, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		2, 20, 100, 200, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		2, 10, 200, 100, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		2, 10, 100, 300, 150)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		-1, -1, -1, -1, -1)
	f.Add(pkgIDs[len(pkgIDs)-1], 1,
		16, 16, 256, 256, 256)
	f.Fuzz(func(t *testing.T, pkg, clos, epp, pp, minf, maxf, desiredf int) {
		expectedInfo := &SstClosInfo{
			EPP:                  epp,
			ProportionalPriority: pp,
			MinFreq:              minf,
			MaxFreq:              maxf,
			DesiredFreq:          desiredf,
		}
		err := ClosSetup(pkgs[pkg], clos, expectedInfo)
		if err != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
		info, err := getSinglePackageInfo(pkgs[pkg].pkg)
		if err != nil {
			t.Errorf("failed to get package info: %v", err)
		}
		if !cmp.Equal(info.ClosInfo[clos], *expectedInfo) {
			t.Errorf("CLOS not configured correctly, expected %v, got %v", expectedInfo, info.ClosInfo[clos])
		}
	})
}
