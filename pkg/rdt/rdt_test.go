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
	"io/ioutil"
	stdlog "log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/intel/goresctrl/pkg/utils"
	testdata "github.com/intel/goresctrl/test/data"
)

const mockGroupPrefix string = "goresctrl."

type mockResctrlFs struct {
	t *testing.T

	origDir string
	baseDir string
}

func newMockResctrlFs(t *testing.T, name, mountOpts string) (*mockResctrlFs, error) {
	var err error
	m := &mockResctrlFs{t: t}

	m.origDir = testdata.Path(name)
	m.baseDir, err = ioutil.TempDir("", "goresctrl.test.")
	if err != nil {
		return nil, err
	}

	// Create resctrl filesystem mock
	m.copyFromOrig("", "")

	// Create mountinfo mock
	mountInfoPath = filepath.Join(m.baseDir, "mounts")
	resctrlPath := filepath.Join(m.baseDir, "resctrl")
	data := "resctrl " + resctrlPath + " resctrl " + mountOpts + " 0 0\n"
	if err := ioutil.WriteFile(mountInfoPath, []byte(data), 0644); err != nil {
		m.delete()
		return nil, err
	}
	return m, nil
}

func (m *mockResctrlFs) delete() {
	if err := os.RemoveAll(m.baseDir); err != nil {
		m.t.Fatalf("failed to delete mock resctrl fs: %v", err)
	}
}

func (m *mockResctrlFs) initMockMonGroup(class, name string) {
	m.copyFromOrig(filepath.Join("mon_groups", "example"), filepath.Join(mockGroupPrefix+class, "mon_groups", mockGroupPrefix+name))
}

func (m *mockResctrlFs) copyFromOrig(relSrc, relDst string) {
	absSrc := filepath.Join(m.origDir, relSrc)
	if s, err := os.Stat(absSrc); err != nil {
		m.t.Fatalf("%v", err)
	} else if s.IsDir() {
		absSrc = filepath.Join(absSrc, ".")
	}

	absDst := filepath.Join(m.baseDir, "resctrl", relDst)
	cmd := exec.Command("cp", "-r", absSrc, absDst)
	if err := cmd.Run(); err != nil {
		m.t.Fatalf("failed to copy mock data %q -> %q: %v", absSrc, absDst, err)
	}
}

func (m *mockResctrlFs) verifyTextFile(relPath, content string) {
	verifyTextFile(m.t, filepath.Join(m.baseDir, "resctrl", relPath), content)
}

func verifyTextFile(t *testing.T, path, content string) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %q: %v", path, err)
	}
	if string(data) != content {
		t.Fatalf("unexpected content in %q\nexpected:\n  %q\nfound:\n  %q", path, content, data)
	}
}

func parseTestConfig(t *testing.T, data string) *Config {
	c := &Config{}
	if err := yaml.Unmarshal([]byte(data), c); err != nil {
		t.Fatalf("failed to parse rdt config: %v", err)
	}
	return c
}

// TestRdt tests the rdt public API, i.e. exported functionality of the package
func TestRdt(t *testing.T) {
	const rdtTestConfig string = `
partitions:
  priority:
    l3Allocation:
      all: 60%
    mbAllocation:
      all: [100%]
    classes:
      Guaranteed:
        l3schema:
          all: 100%
  default:
    l3Allocation:
      all: 40%
    mbAllocation:
      all: [100%]
    classes:
      Burstable:
        l3schema:
          all: 100%
        mbschema:
          all: [66%]
      BestEffort:
        l3schema:
          all: 66%
        mbschema:
          all: [33%]
`

	verifyGroupNames := func(a interface{}, b []string) {
		var names []string

		switch v := a.(type) {
		case []CtrlGroup:
			for _, g := range v {
				names = append(names, g.Name())
			}
		case []MonGroup:
			for _, g := range v {
				names = append(names, g.Name())
			}
		default:
			t.Errorf("Invalid type '%T' in verifyGroupNames()", a)
			return
		}
		if len(b) == 0 && len(names) == 0 {
			return
		}
		if !cmp.Equal(names, b) {
			t.Errorf("unexpected class/group names: expected %s got %s", b, names)
		}
	}

	// Set group remove function so that mock groups can be removed
	groupRemoveFunc = os.RemoveAll

	//
	// 1. test uninitialized interface
	//
	SetLogger(NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt-test-1 ] ", 0)))

	if err := SetConfig(&Config{}); err == nil {
		t.Errorf("setting config on uninitialized rdt succeeded unexpectedly")

	}
	if classes := GetClasses(); len(classes) != 0 {
		t.Errorf("uninitialized rdt contains classes %s", classes)
	}
	if _, ok := GetClass(""); ok {
		t.Errorf("expected to not get a class with empty name")
	}
	if MonSupported() {
		t.Errorf("unitialized rdt claims monitoring to be supported")
	}
	if features := GetMonFeatures(); len(features) != 0 {
		t.Errorf("uninitialized rdt returned monitoring features %s", features)
	}

	//
	// 2. Test setting up RDT with L3 L3_MON and MB support
	//
	mockFs, err := newMockResctrlFs(t, "resctrl.full", "")
	if err != nil {
		t.Fatalf("failed to set up mock resctrl fs: %v", err)
	}
	defer mockFs.delete()

	conf := parseTestConfig(t, rdtTestConfig)
	if err := Initialize(mockGroupPrefix); err != nil {
		t.Fatalf("rdt initialization failed: %v", err)
	}

	// Check that existing groups were read correctly on init
	classes := GetClasses()
	verifyGroupNames(classes, []string{"Guaranteed", "SYSTEM_DEFAULT", "Stale"})

	cls, _ := GetClass("SYSTEM_DEFAULT")
	verifyGroupNames(cls.GetMonGroups(), []string{})
	cls, _ = GetClass("Guaranteed")
	verifyGroupNames(cls.GetMonGroups(), []string{"predefined_group_live"})

	// Check that confiouration succeeds
	if err := SetConfig(conf); err != nil {
		t.Fatalf("rdt configuration failed: %v", err)
	}

	// Check that SetLogger() takes effect in the control interface, too
	SetLogger(NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt-test-2 ] ", 0)))
	if p := rdt.Logger.(*logger).Prefix(); p != "[ rdt-test-2 ] " {
		t.Errorf("unexpected logger prefix %q", p)
	}

	// Check that the path() and relPath() methods work correctly
	if p := rdt.classes["Guaranteed"].path("foo"); p != filepath.Join(mockFs.baseDir, "resctrl", "goresctrl.Guaranteed", "foo") {
		t.Errorf("path() returned wrong path %q", p)
	}
	if p := rdt.classes["Guaranteed"].relPath("foo"); p != filepath.Join("goresctrl.Guaranteed", "foo") {
		t.Errorf("relPath() returned wrong path %q", p)
	}

	// Verify that ctrl groups are correctly configured
	mockFs.verifyTextFile(rdt.classes["BestEffort"].relPath("schemata"),
		"L3:0=3f;1=3f;2=3f;3=3f\nMB:0=33;1=33;2=33;3=33\n")
	mockFs.verifyTextFile(rdt.classes["Burstable"].relPath("schemata"),
		"L3:0=ff;1=ff;2=ff;3=ff\nMB:0=66;1=66;2=66;3=66\n")
	mockFs.verifyTextFile(rdt.classes["Guaranteed"].relPath("schemata"),
		"L3:0=fff00;1=fff00;2=fff00;3=fff00\nMB:0=100;1=100;2=100;3=100\n")

	// Verify that existing goresctrl monitor groups were removed
	for _, cls := range []string{RootClassName, "Guaranteed"} {
		files, _ := ioutil.ReadDir(rdt.classes[cls].path("mon_groups"))
		for _, f := range files {
			if strings.HasPrefix(mockGroupPrefix, f.Name()) {
				t.Errorf("unexpected monitor group found %q", f.Name())
			}
		}
	}

	// Verify GetClasses
	classes = GetClasses()
	verifyGroupNames(classes, []string{"BestEffort", "Burstable", "Guaranteed", "SYSTEM_DEFAULT"})

	// Verify assigning pids to classes (ctrl groups)
	cls, _ = GetClass("Guaranteed")
	if n := cls.Name(); n != "Guaranteed" {
		t.Errorf("CtrlGroup.Name() returned %q, expected %q", n, "Guaranteed")
	}

	pids := []string{"10", "11", "12"}
	if err := cls.AddPids(pids...); err != nil {
		t.Errorf("AddPids() failed: %v", err)
	}
	if p, err := cls.GetPids(); err != nil {
		t.Errorf("GetPids() failed: %v", err)
	} else if !cmp.Equal(p, pids) {
		t.Errorf("GetPids() returned %s, expected %s", p, pids)
	}

	mockFs.verifyTextFile(rdt.classes["Guaranteed"].relPath("tasks"), "10\n11\n12\n")

	// Verify MonSupported and GetMonFeatures
	if !MonSupported() {
		t.Errorf("MonSupported() returned false, expected true")
	}
	expectedMonFeatures := map[MonResource][]string{MonResourceL3: []string{"llc_occupancy", "mbm_local_bytes", "mbm_total_bytes"}}
	if features := GetMonFeatures(); !cmp.Equal(features, expectedMonFeatures) {
		t.Fatalf("GetMonFeatures() returned %v, expected %v", features, expectedMonFeatures)
	}

	// Test creating monitoring groups
	cls, _ = GetClass("Guaranteed")
	mgName := "test_group"
	mgAnnotations := map[string]string{"a_key": "a_value"}
	mg, err := cls.CreateMonGroup(mgName, mgAnnotations)
	if err != nil {
		t.Errorf("creating mon group failed: %v", err)
	}
	if n := mg.Name(); n != mgName {
		t.Errorf("MonGroup.Name() returned %q, expected %q", n, mgName)
	}
	if a := mg.GetAnnotations(); !cmp.Equal(a, mgAnnotations) {
		t.Errorf("MonGroup.GetAnnotations() returned %s, expected %s", a, mgAnnotations)
	}
	if n := mg.Parent().Name(); n != "Guaranteed" {
		t.Errorf("MonGroup.Parent().Name() returned %q, expected %q", n, "Guaranteed")
	}

	if _, ok := cls.GetMonGroup("non-existing-group"); ok {
		t.Errorf("unexpected success when querying non-existing group")
	}
	if _, ok := cls.GetMonGroup(mgName); !ok {
		t.Errorf("unexpected error when querying mon group: %v", err)
	}

	verifyGroupNames(cls.GetMonGroups(), []string{"predefined_group_live", mgName})

	mgPath := rdt.classes["Guaranteed"].path("mon_groups", "goresctrl."+mgName)
	if _, err := os.Stat(mgPath); err != nil {
		t.Errorf("mon group directory not found: %v", err)
	}

	// Check that the monGroup.path() and relPath() methods work correctly
	mgi := rdt.classes["Guaranteed"].monGroups[mgName]
	if p := mgi.path("foo"); p != filepath.Join(mockFs.baseDir, "resctrl", "goresctrl.Guaranteed", "mon_groups", "goresctrl."+mgName, "foo") {
		t.Errorf("path() returned wrong path %q", p)
	}
	if p := mgi.relPath("foo"); p != filepath.Join("goresctrl.Guaranteed", "mon_groups", "goresctrl."+mgName, "foo") {
		t.Errorf("relPath() returned wrong path %q", p)
	}

	// Test deleting monitoring groups
	if err := cls.DeleteMonGroup(mgName); err != nil {
		t.Errorf("unexpected error when deleting mon group: %v", err)
	}
	if _, ok := cls.GetMonGroup("non-existing-group"); ok {
		t.Errorf("unexpected success when querying deleted group")
	}
	if _, err := os.Stat(mgPath); !os.IsNotExist(err) {
		t.Errorf("unexpected error when checking directory of deleted mon group: %v", err)
	}

	// Verify assigning pids to monitor group
	mgName = "test_group_2"
	mockFs.initMockMonGroup("Guaranteed", mgName)
	cls, _ = GetClass("Guaranteed")
	mg, _ = cls.CreateMonGroup(mgName, nil)

	pids = []string{"10"}
	if err := mg.AddPids(pids...); err != nil {
		t.Errorf("MonGroup.AddPids() failed: %v", err)
	}
	if p, err := mg.GetPids(); err != nil {
		t.Errorf("MonGroup.GetPids() failed: %v", err)
	} else if !cmp.Equal(p, pids) {
		t.Errorf("MonGroup.GetPids() returned %s, expected %s", p, pids)
	}
	mockFs.verifyTextFile(rdt.classes["Guaranteed"].monGroups[mgName].relPath("tasks"), "10\n")

	// Verify monitoring functionality
	expected := MonData{
		L3: MonL3Data{
			0: MonLeafData{
				"llc_occupancy":   1,
				"mbm_local_bytes": 2,
				"mbm_total_bytes": 3,
			},
			1: MonLeafData{
				"llc_occupancy":   11,
				"mbm_local_bytes": 12,
				"mbm_total_bytes": 13,
			},
			2: MonLeafData{
				"llc_occupancy":   21,
				"mbm_local_bytes": 22,
				"mbm_total_bytes": 23,
			},
			3: MonLeafData{
				"llc_occupancy":   31,
				"mbm_local_bytes": 32,
				"mbm_total_bytes": 33,
			},
		},
	}
	md := mg.GetMonData()
	if !cmp.Equal(md, expected) {
		t.Errorf("unexcpected monitoring data\nexpected:\n%s\nreceived:\n%s", utils.DumpJSON(expected), utils.DumpJSON(md))
	}
}

// TestConfig tests configuration parsing and resolving
func TestConfig(t *testing.T) {
	type Schemata struct {
		l3     string
		l3code string
		l3data string
		mb     string
	}

	type TC struct {
		name        string
		fs          string
		fsMountOpts string
		config      string
		configErrRe string
		schemata    map[string]Schemata
	}

	tcs := []TC{
		// Testcase
		TC{
			name: "Complex config",
			fs:   "resctrl.full",
			config: `
partitions:
  part-1:
    l3Allocation:
      all: 60%
      1: "0xff000"
      2: "9-15"
    mbAllocation:
      all: [100%]
    classes:
      class-1:
        l3schema: 100%
      class-2:
        l3schema:
          all: 100%
          0-1: 10%
          2: "0x70"
        mbschema:
          all: [40%]
          3: [10%]
  part-2:
    l3Allocation:
      all: 39%
      1: "0-10"
      2: "0-6"
    mbAllocation:
      all: [50%]
      1: [80%]
      2: [100%]
    classes:
      class-3:
        l3schema: 100%
        mbschema:
          all: [40%]
          0: [80%]
      class-4:
        l3schema: 50%
        mbschema: [100%]
      SYSTEM_DEFAULT:
        l3schema: 60%
        mbschema: [60%]
  part-3:
    l3Allocation:
      all: 1%
      1: "0x800"
      2: "7,8"
    mbAllocation: [20%]
    classes:
      class-5:
        l3schema: 100%
        mbschema:
          all: [100%]
          0: [1%]
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3: "0=fff;1=ff000;2=fe00;3=fff",
					mb: "0=100;1=100;2=100;3=100",
				},
				"class-2": Schemata{
					l3: "0=3;1=1000;2=e000;3=fff",
					mb: "0=40;1=40;2=40;3=10",
				},
				"class-3": Schemata{
					l3: "0=7f000;1=7ff;2=7f;3=7f000",
					mb: "0=40;1=32;2=40;3=20",
				},
				"class-4": Schemata{
					l3: "0=f000;1=3f;2=f;3=f000",
					mb: "0=50;1=80;2=100;3=50",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3: "0=1f000;1=7f;2=1f;3=1f000",
					mb: "0=30;1=48;2=60;3=30",
				},
				"class-5": Schemata{
					l3: "0=80000;1=800;2=180;3=80000",
					mb: "0=10;1=20;2=20;3=20",
				},
			},
		},
		// Testcase
		TC{
			name: "L3 CDP disabled",
			fs:   "resctrl.nomb",
			config: `
partitions:
  part-1:
    l3Allocation:
      0,1:
        unified: 60%
        code: 70%
        data: 50%
      2,3: 40%
    classes:
      class-1:
  part-2:
    l3Allocation:
      0,1:
        unified: 40%
        code: 30%
        data: 50%
      2,3: 60%
    classes:
      class-2:
      SYSTEM_DEFAULT:
        l3Schema:
          all: 100%
          3:
            unified: 80%
            code: 60%
            data: 90%

`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3: "0=fff;1=fff;2=ff;3=ff",
				},
				"class-2": Schemata{
					l3: "0=ff000;1=ff000;2=fff00;3=fff00",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3: "0=ff000;1=ff000;2=fff00;3=3ff00",
				},
			},
		},
		// Testcase
		TC{
			name: "L3 CDP enabled",
			fs:   "resctrl.nomb.cdp",
			config: `
partitions:
  part-1:
    l3Allocation:
      0,1:
        unified: 60%
        code: 70%
        data: 50%
      2,3: 40%
    classes:
      class-1:
  part-2:
    l3Allocation:
      0,1:
        unified: 40%
        code: 30%
        data: 50%
      2,3: 60%
    classes:
      class-2:
      SYSTEM_DEFAULT:
        l3Schema:
          all: 100%
          3:
            unified: 80%
            code: 60%
            data: 90%

`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3code: "0=3fff;1=3fff;2=ff;3=ff",
					l3data: "0=3ff;1=3ff;2=ff;3=ff",
				},
				"class-2": Schemata{
					l3code: "0=fc000;1=fc000;2=fff00;3=fff00",
					l3data: "0=ffc00;1=ffc00;2=fff00;3=fff00",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3code: "0=fc000;1=fc000;2=fff00;3=ff00",
					l3data: "0=ffc00;1=ffc00;2=fff00;3=7ff00",
				},
			},
		},
		// Testcase
		TC{
			name: "L3 optional",
			fs:   "resctrl.nol3",
			config: `
options:
  l3:
    optional: true
partitions:
  part-1:
    l3Allocation: 100%
    mbAllocation: [100%]
    classes:
      class-1:
        l3schema: 20%
        mbschema: [50%]
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					mb: "0=50;1=50;2=50;3=50",
				},
				"SYSTEM_DEFAULT": Schemata{
					mb: "0=100;1=100;2=100;3=100",
				},
			},
		},
		// Testcase
		TC{
			name:        "duplicate class names (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `"class-1" defined multiple times`,
			config: `
partitions:
  part-1:
    classes:
      class-1:
  part-2:
    classes:
      class-1:
`,
		},
		// Testcase
		TC{
			name:        "Invalid allocation schema (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": invalid structure of allocation schema`,
			config: `
partitions:
  part-1:
    l3Allocation: 0x12
`,
		},
		// Testcase
		TC{
			name:        "Invalid cache ids (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": rdt: invalid integer "a"`,
			config: `
partitions:
  part-1:
    l3Allocation:
      a: 100%
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid allocation schema #1, invalid shorthand schema (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": invalid structure of l3Schema`,
			config: `
partitions:
  part-1:
    l3Allocation: [100%]
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid allocation schema #2, invalid per-type schema (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": not a string value`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        unified: [100%]
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid allocation schema #3, missing unified (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": 'unified' not specified in l3Schema`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        data: 100%
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid allocation schema #4, missing code (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": 'data' specified but missing 'code' from l3Schema`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        unified: 100%
        data: 100%
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid allocation schema #5, missing data (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": 'code' specified but missing 'data' from l3Schema`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        unified: 100%
        code: 100%
`,
		},
		// Testcase
		TC{
			name:        "L3 required (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `L3 cache allocation for "class-1" specified in configuration but not supported by system`,
			config: `
partitions:
  part-1:
    l3Allocation: 100%
    classes:
      class-1:
        l3schema: 20%
`,
		},
		// Testcase
		TC{
			name: "MB optional",
			fs:   "resctrl.nomb",
			config: `
options:
  mb:
    optional: true
partitions:
  part-1:
    l3Allocation: 100%
    mbAllocation: [100%]
    classes:
      class-1:
        l3schema: 0-7
        mbschema: [50%]
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3: "0=ff;1=ff;2=ff;3=ff",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3: "0=fffff;1=fffff;2=fffff;3=fffff",
				},
			},
		},
		// Testcase
		TC{
			name:        "MB required (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `memory bandwidth allocation for "class-1" specified in configuration but not supported by system`,
			config: `
partitions:
  part-1:
    mbAllocation: [100%]
    classes:
      class-1:
        mbschema: [50%]
`,
		},
		// Testcase
		TC{
			name:        "L3 mix rel and abs allocation in partition (fail)",
			fs:          "resctrl.full",
			configErrRe: "error resolving L3 allocation for cache id 0: mixing absolute and relative allocations between partitions not supported",
			config: `
partitions:
  part-1:
    l3Allocation: "0xff"
  part-2:
    l3Allocation: 50%
`,
		},
		// Testcase
		TC{
			name:        "L3 mix rel and abs allocation in partition #2 (fail)",
			fs:          "resctrl.full",
			configErrRe: "error resolving L3 allocation for cache id 0: mixing relative and absolute allocations between partitions not supported",
			config: `
partitions:
  part-1:
    l3Allocation: 50%
  part-2:
    l3Allocation: "0xff"
`,
		},
		// Testcase
		TC{
			name: "L3 mix rel and abs allocation in classes",
			fs:   "resctrl.nomb",
			config: `
partitions:
  part-1:
    l3Allocation: 100%
    classes:
      class-1:
        l3schema:
            all: 100%
            1: 50%
      class-2:
        l3schema:
            all: 50%
            1: "0x7"
            2: "1-2"
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3: "0=fffff;1=3ff;2=fffff;3=fffff",
				},
				"class-2": Schemata{
					l3: "0=3ff;1=7;2=6;3=3ff",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3: "0=fffff;1=fffff;2=fffff;3=fffff",
				},
			},
		},
		// Testcase
		TC{
			name:        "L3 partition non-contiguous bitmask (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": invalid cache bitmask "0x2f": more than one continuous block of ones`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all: "100%"
      1: "0x2f"
`,
		},
		// Testcase
		TC{
			name:        "L3 overlapping partitions (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `overlapping L3 partition allocation requests for cache id 2`,
			config: `
partitions:
  part-1:
    l3Allocation: "0xff"
  part-2:
    l3Allocation:
      all: "0xff00"
      2: "0xff80"
`,
		},
		// Testcase
		TC{
			name:        "L3 nan percentage in partition (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": strconv.ParseUint: parsing "1f": invalid syntax`,
			config: `
partitions:
  part-1:
    l3Allocation: "1f%"
`,
		},
		// Testcase
		TC{
			name:        "L3 percentage range in partition (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `invalid configuration: percentage ranges in partition allocation not supported`,
			config: `
partitions:
  part-1:
    l3Allocation: "50-100%"
`,
		},
		// Testcase
		TC{
			name:        "L3 missing for one partition (fail)",
			fs:          "resctrl.full",
			configErrRe: `invalid configuration: L3 allocation only specified for a subset of partitions`,
			config: `
partitions:
  part-1:
    l3Allocation: "50%"
    mbAllocation: ["100%"]
  part-2:
    mbAllocation: ["100%"]
`,
		},
		// Testcase
		TC{
			name:        "L3 percentage over 100 in partition (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `failed to parse L3 allocation request for partition "part-1": invalid percentage value "101%"`,
			config: `
partitions:
  part-1:
    l3Allocation: "101%"
`,
		},
		// Testcase
		TC{
			name:        "L3 missing cdp (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `some partition\(s\) missing l3 "code" allocation request for cache id [0-3]`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        unified: "50%"
        code: "40%"
        data: "60%"
  part-2:
    l3Allocation: "50%"
`,
		},
		// Testcase
		TC{
			name:        "L3 total percentage over 100 (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `accumulated L3 "data" partition allocation requests for cache id [0-3] exceeds 100%`,
			config: `
partitions:
  part-1:
    l3Allocation:
      all:
        unified: "50%"
        code: "40%"
        data: "60%"
  part-2:
    l3Allocation:
      all:
        unified: "50%"
        code: "40%"
        data: "60%"
`,
		},
		// Testcase
		TC{
			name:        "L3 class allocation does not fit partition (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `bitmask 0x1ff00 \(0x1ff << 8\) does not fit basemask 0xff00`,
			config: `
partitions:
  part-1:
    l3Allocation: "0xff00"
    classes:
      class-1:
        l3schema: "0x1ff"
`,
		},
		// Testcase
		TC{
			name: "L3 min cbm bits is respected",
			fs:   "resctrl.nomb",
			config: `
partitions:
  part-1:
    l3Allocation: "100%"
    classes:
      class-1:
        l3schema:
          all: "1%"
          1-2: "99-100%"
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					l3: "0=3;1=c0000;2=c0000;3=3",
				},
				"SYSTEM_DEFAULT": Schemata{
					l3: "0=fffff;1=fffff;2=fffff;3=fffff",
				},
			},
		},
		// Testcase
		TC{
			name:        "L3 too few bits (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `bitmask 0x1ff00 \(0x1ff << 8\) does not fit basemask 0xff00`,
			config: `
partitions:
  part-1:
    l3Allocation: "0xff00"
    classes:
      class-1:
        l3schema: "0x1ff"
`,
		},
		// Testcase
		TC{
			name:        "L3 invalid percentage range in class (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `invalid configuration: failed to resolve L3 allocation for class "class-1": invalid percentage range`,
			config: `
partitions:
  part-1:
    l3Allocation: "100%"
    classes:
      class-1:
        l3schema: "0-101%"
`,
		},
		// Testcase
		TC{
			name:        "L3 missing from partition (fail)",
			fs:          "resctrl.nomb",
			configErrRe: `L3 allocation missing from partition "part-1"`,
			config: `
partitions:
  part-1:
    classes:
      class-1:
        l3schema: "100%"
`,
		},
		// Testcase
		TC{
			name: "MB allocation under minimum",
			fs:   "resctrl.nol3",
			config: `
partitions:
  part-1:
    mbAllocation: ["1%"]
    classes:
      class-1:
        mbSchema: ["100%"]
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					mb: "0=10;1=10;2=10;3=10",
				},
				"SYSTEM_DEFAULT": Schemata{
					mb: "0=100;1=100;2=100;3=100",
				},
			},
		},
		// Testcase
		TC{
			name:        "MB nan percentage value in partition (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `failed to resolve MB allocation for partition "part-1": strconv.ParseUint: parsing "xyz"`,
			config: `
partitions:
  part-1:
    mbAllocation: ["xyz%"]
`,
		},
		// Testcase
		TC{
			name:        "MB invalid percentage value in class (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `failed to resolve MB allocation for class "class-1":.*invalid syntax`,
			config: `
partitions:
  part-1:
    mbAllocation: ["100%"]
    classes:
      class-1:
        mbschema: ["1a%"]
`,
		},
		// Testcase
		TC{
			name:        "MB class allocation not a list (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `failed to resolve MB allocation for class "class-1": not a list value`,
			config: `
partitions:
  part-1:
    mbAllocation: ["100%"]
    classes:
      class-1:
        mbschema: "100%"
`,
		},
		// Testcase
		TC{
			name:        "MB missing percentage value (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `missing '%' value from mbSchema`,
			config: `
partitions:
  part-1:
    mbAllocation: ["100MBps"]
`,
		},
		// Testcase
		TC{
			name:        "MB missing from partition (fail)",
			fs:          "resctrl.nol3",
			configErrRe: `MB allocation missing from partition "part-1"`,
			config: `
partitions:
  part-1:
    classes:
      class-1:
        mbschema: ["100%"]
`,
		},
		// Testcase
		TC{
			name:        "MB MBps",
			fs:          "resctrl.nol3.mbps",
			fsMountOpts: "mba_MBps",
			config: `
partitions:
  part-1:
    mbAllocation: ["50%", "1000MBps"]
    classes:
      class-1:
        mbschema: ["100%", "1500MBps"]
  part-2:
    mbAllocation:
      all: ["1000MBps"]
      # Unsupported values should just be ignored
      0,1: [50, "1GBps", "500MBps"]
    classes:
      class-2:
        mbschema: ["750MBps"]
`,
			schemata: map[string]Schemata{
				"class-1": Schemata{
					mb: "0=1000;1=1000;2=1000;3=1000",
				},
				"class-2": Schemata{
					mb: "0=500;1=500;2=750;3=750",
				},
				"SYSTEM_DEFAULT": Schemata{
					mb: "0=4294967295;1=4294967295;2=4294967295;3=4294967295",
				},
			},
		},
		// Testcase
		TC{
			name:        "MB nan MBps value (fail)",
			fs:          "resctrl.nol3.mbps",
			fsMountOpts: "mba_MBps",
			configErrRe: `failed to resolve MB allocation for partition "part-1":.* invalid syntax`,
			config: `
partitions:
  part-1:
    mbAllocation: ["0xffMBps"]
`,
		},
		// Testcase
		TC{
			name:        "MB missing MBps value (fail)",
			fs:          "resctrl.nol3.mbps",
			fsMountOpts: "mba_MBps",
			configErrRe: `missing 'MBps' value from mbSchema`,
			config: `
partitions:
  part-1:
    mbAllocation: ["100%"]
`,
		},
	}

	verifySchemata := func(tc *TC) {
		for n, s := range tc.schemata {
			expected := ""
			if s.l3 != "" {
				expected += "L3:" + s.l3 + "\n"
			}
			if s.l3code != "" {
				expected += "L3CODE:" + s.l3code + "\n"
			}
			if s.l3data != "" {
				expected += "L3DATA:" + s.l3data + "\n"
			}
			if s.mb != "" {
				expected += "MB:" + s.mb + "\n"
			}
			if c, ok := rdt.classes[n]; !ok {
				t.Fatalf("verifySchemata: class %q does not exists in %v", n, rdt.classes)
			} else {
				verifyTextFile(t, c.path("schemata"), expected)
			}
		}

		if len(tc.schemata) != len(rdt.classes) {
			var a, b []string
			for n := range tc.schemata {
				a = append(a, n)
			}
			for n := range rdt.classes {
				b = append(b, n)
			}
			t.Fatalf("unexpected set of classes: expected %v, got %v", a, b)
		}
	}

	// Set group remove function so that mock groups can be removed
	groupRemoveFunc = os.RemoveAll

	for _, tc := range tcs {
		t.Logf("Running test case %q", tc.name)

		mockFs, err := newMockResctrlFs(t, tc.fs, tc.fsMountOpts)
		if err != nil {
			t.Fatalf("failed to set up mock resctrl fs: %v", err)
		}
		defer mockFs.delete()

		conf := parseTestConfig(t, tc.config)
		confDataOld, err := yaml.Marshal(conf)
		if err != nil {
			t.Fatalf("marshalling config failed: %v", err)
		}

		if err := Initialize(mockGroupPrefix); err != nil {
			t.Fatalf("resctrl initialization failed: %v", err)
		}

		err = SetConfig(conf)
		if tc.configErrRe != "" {
			if err == nil {
				t.Fatalf("resctrl configuration succeeded unexpectedly")
			} else {
				m, e := regexp.MatchString(tc.configErrRe, err.Error())
				if e != nil {
					t.Fatalf("error in regexp matching: %v", e)
				}
				if !m {
					t.Fatalf("unexpected error message:\n  %q\n  does NOT match regexp\n  %q", err.Error(), tc.configErrRe)
				}
			}
		} else {
			if err != nil {
				t.Fatalf("resctrl configuration failed: %v", err)
			}
			verifySchemata(&tc)
		}

		if confDataNew, err := yaml.Marshal(conf); err != nil {
			t.Fatalf("marshalling config failed: %v", err)
		} else if !cmp.Equal(confDataNew, confDataOld) {
			t.Fatalf("SetConfig altered config data:\n%s\nVS.\n%s", confDataOld, confDataNew)
		}
	}
}

func TestBitMap(t *testing.T) {
	// Test ListStr()
	testSet := map[Bitmask]string{
		0x0:                "",
		0x1:                "0",
		0x2:                "1",
		0xf:                "0-3",
		0x555:              "0,2,4,6,8,10",
		0xaaa:              "1,3,5,7,9,11",
		0x1d1a:             "1,3-4,8,10-12",
		0xffffffffffffffff: "0-63",
	}
	for i, s := range testSet {
		// Test conversion to string
		listStr := i.ListStr()
		if listStr != s {
			t.Errorf("from %#x expected %q, got %q", i, s, listStr)
		}

		// Test conversion from string
		b, err := ListStrToBitmask(s)
		if err != nil {
			t.Errorf("unexpected err when converting %q: %v", s, err)
		}
		if b != i {
			t.Errorf("from %q expected %#x, got %#x", s, i, b)
		}
	}

	// Negative tests for ListStrToBitmask
	negTestSet := []string{
		",",
		"-",
		"1,",
		",12",
		"-4",
		"0-",
		"13-13",
		"14-13",
		"a-2",
		"b",
		"3-c",
		"64",
		"1,2,,3",
		"1,2,3-",
	}
	for _, s := range negTestSet {
		b, err := ListStrToBitmask(s)
		if err == nil {
			t.Errorf("expected err but got %#x when converting %q", b, s)
		}
	}

	// Test MarshalJSON
	if s, err := Bitmask(10).MarshalJSON(); err != nil {
	} else if string(s) != `"0xa"` {
		t.Errorf(`expected "0xa" but returned %s`, s)
	}
}

func TestListStrToArray(t *testing.T) {
	testSet := map[string][]int{
		"":              {},
		"0":             {0},
		"1":             {1},
		"0-3":           {0, 1, 2, 3},
		"4,2,0,6,10,8":  {0, 2, 4, 6, 8, 10},
		"1,3,5,7,9,11":  {1, 3, 5, 7, 9, 11},
		"1,3-4,10-12,8": {1, 3, 4, 8, 10, 11, 12},
	}
	for s, expected := range testSet {
		// Test conversion from string to list of integers
		a, err := listStrToArray(s)
		if err != nil {
			t.Errorf("unexpected error when converting %q: %v", s, err)
		}
		if !cmp.Equal(a, expected) {
			t.Errorf("from %q expected %v, got %v", s, expected, a)
		}
	}

	// Negative test cases
	negTestSet := []string{
		",",
		"-",
		"1,",
		"256",
		"256-257",
		"0-256",
		",12",
		"-4",
		"0-",
		"13-13",
		"14-13",
		"a-2",
		"b",
		"3-c",
		"1,2,,3",
		"1,2,3-",
	}
	for _, s := range negTestSet {
		a, err := listStrToArray(s)
		if err == nil {
			t.Errorf("expected err but got %v when converting %q", a, s)
		}
	}
}

// TestCacheAllocation tests the types implementing cacheAllocation interface
func TestCacheAllocation(t *testing.T) {
	// Need to setup resctrl and initialize because pct allocations need
	// the "info" structure
	mockFs, err := newMockResctrlFs(t, "resctrl.nomb", "")
	if err != nil {
		t.Fatalf("failed to set up mock resctrl fs: %v", err)
	}
	defer mockFs.delete()

	if err := Initialize(mockGroupPrefix); err != nil {
		t.Fatalf("resctrl initialization failed: %v", err)
	}

	// Test absolute allocation
	abs := l3AbsoluteAllocation(0x7)
	if res, err := abs.Overlay(0xf00); err != nil {
		t.Errorf("unexpected error when overlaying l3AbsoluteAllocation: %v", err)
	} else if res != 0x700 {
		t.Errorf("expected 0x700 but got %#x when overlaying l3AbsoluteAllocation", res)
	}

	if _, err := abs.Overlay(0); err == nil {
		t.Errorf("unexpected success when overlaying l3AbsoluteAllocation with empty basemask")
	}

	if _, err := abs.Overlay(0x30); err == nil {
		t.Errorf("unexpected success when overlaying too wide l3AbsoluteAllocation")
	}

	if _, err := abs.Overlay(0xf0f); err == nil {
		t.Errorf("unexpected success when overlaying l3AbsoluteAllocation with non-contiguous basemask")
	}

	if _, err := l3AbsoluteAllocation(0x1).Overlay(0x10); err == nil {
		t.Errorf("unexpected success when overlaying l3AbsoluteAllocation with too small basemask")
	}

	// Test percentage allocation
	info.l3.minCbmBits = 4
	if res, err := (l3PctRangeAllocation{lowPct: 0, highPct: 100}).Overlay(0xff00); err != nil {
		t.Errorf("unexpected error when overlaying l3PctRangeAllocation: %v", err)
	} else if res != 0xff00 {
		t.Errorf("expected 0xff00 but got %#x when overlaying l3PctRangeAllocation", res)
	}
	if res, err := (l3PctRangeAllocation{lowPct: 99, highPct: 100}).Overlay(0xff00); err != nil {
		t.Errorf("unexpected error when overlaying l3PctRangeAllocation: %v", err)
	} else if res != 0xf000 {
		t.Errorf("expected 0xf000 but got %#x when overlaying l3PctRangeAllocation", res)
	}
	if res, err := (l3PctRangeAllocation{lowPct: 0, highPct: 1}).Overlay(0xff00); err != nil {
		t.Errorf("unexpected error when overlaying l3PctRangeAllocation: %v", err)
	} else if res != 0xf00 {
		t.Errorf("expected 0xf00 but got %#x when overlaying l3PctRangeAllocation", res)
	}
	if res, err := (l3PctRangeAllocation{lowPct: 20, highPct: 30}).Overlay(0x3ff00); err != nil {
		t.Errorf("unexpected error when overlaying l3PctRangeAllocation: %v", err)
	} else if res != 0xf00 {
		t.Errorf("expected 0xf00 but got %#x when overlaying l3PctRangeAllocation", res)
	}
	if res, err := (l3PctRangeAllocation{lowPct: 30, highPct: 60}).Overlay(0xf00); err != nil {
		t.Errorf("unexpected error when overlaying l3PctRangeAllocation: %v", err)
	} else if res != 0xf00 {
		t.Errorf("expected 0xf00 but got %#x when overlaying l3PctRangeAllocation", res)
	}
	if _, err := (l3PctRangeAllocation{lowPct: 20, highPct: 10}).Overlay(0xff00); err == nil {
		t.Errorf("unexpected success when overlaying l3PctRangeAllocation of invalid percentage range")
	}
	if _, err := (l3PctRangeAllocation{lowPct: 0, highPct: 100}).Overlay(0); err == nil {
		t.Errorf("unexpected success when overlaying l3PctRangeAllocation of invalid percentage range")
	}
}

func TestParseCacheAllocation(t *testing.T) {
	// Test percentage
	if a, err := parseCacheAllocation("10%"); err != nil {
		t.Errorf("unexpected error when parsing cache allocation: %v", err)
	} else if a != l3PctAllocation(10) {
		t.Errorf("expected 10%% but got %d%%", a)
	}
	if _, err := parseCacheAllocation("1a%"); err == nil {
		t.Errorf("unexpected success when parsing percentage cache allocation")
	}
	if _, err := parseCacheAllocation("101%"); err == nil {
		t.Errorf("unexpected success when parsing percentage cache allocation")
	}

	// Test percentage ranges
	if a, err := parseCacheAllocation("10-20%"); err != nil {
		t.Errorf("unexpected error when parsing cache allocation: %v", err)
	} else if a != (l3PctRangeAllocation{lowPct: 10, highPct: 20}) {
		t.Errorf("expected {10 20} but got %v", a)
	}
	if _, err := parseCacheAllocation("a-100%"); err == nil {
		t.Errorf("unexpected success when parsing percentage range cache allocation")
	}
	if _, err := parseCacheAllocation("0-1f%"); err == nil {
		t.Errorf("unexpected success when parsing percentage range cache allocation")
	}
	if _, err := parseCacheAllocation("20-10%"); err == nil {
		t.Errorf("unexpected success when parsing percentage range cache allocation")
	}
	if _, err := parseCacheAllocation("20-101%"); err == nil {
		t.Errorf("unexpected success when parsing percentage range cache allocation")
	}

	// Test bitmask
	info = &resctrlInfo{}
	info.l3.minCbmBits = 2
	if a, err := parseCacheAllocation("0xf0"); err != nil {
		t.Errorf("unexpected error when parsing cache allocation: %v", err)
	} else if a != l3AbsoluteAllocation(0xf0) {
		t.Errorf("expected 0xf0 but got %#x", a)
	}
	if _, err := parseCacheAllocation("0x11"); err == nil {
		t.Errorf("unexpected success when parsing bitmask cache allocation")
	}
	if _, err := parseCacheAllocation("0xg"); err == nil {
		t.Errorf("unexpected success when parsing bitmask cache allocation")
	}

	// Test bit numbers
	if a, err := parseCacheAllocation("3,4,5-7,8"); err != nil {
		t.Errorf("unexpected error when parsing cache allocation: %v", err)
	} else if a != l3AbsoluteAllocation(0x1f8) {
		t.Errorf("expected 0x1f8 but got %#x", a)
	}
	if _, err := parseCacheAllocation("3,5"); err == nil {
		t.Errorf("unexpected success when parsing bitmask cache allocation")
	}
	if _, err := parseCacheAllocation("1"); err == nil {
		t.Errorf("unexpected success when parsing bitmask cache allocation")
	}
	if _, err := parseCacheAllocation("3-x"); err == nil {
		t.Errorf("unexpected success when parsing bitmask cache allocation")
	}
}
