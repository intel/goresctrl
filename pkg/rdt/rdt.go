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
	"errors"
	"fmt"
	"io/ioutil"
	stdlog "log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/marquiz/goresctrl/pkg/utils"
)

const (
	// RootClassName is the name we use in our config for the special class
	// that configures the "root" resctrl group of the system
	RootClassName = "SYSTEM_DEFAULT"
)

type control struct {
	Logger

	resctrlGroupPrefix string
	conf               config
	rawConf            Config
	info               info
	classes            map[string]*ctrlGroup
}

var log Logger = NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt ] ", 0))

var rdt *control = &control{
	Logger: log,
}

// CtrlGroup defines the interface of one goresctrl managed RDT class
type CtrlGroup interface {
	ResctrlGroup

	// CreateMonGroup creates a new monitoring group under the class.
	CreateMonGroup(name string, annotations map[string]string) (MonGroup, error)

	// DeleteMonGroup deletes a monitoring group from the class.
	DeleteMonGroup(name string) error

	// GetMonGroup returns a specific monitoring group under the class
	GetMonGroup(name string) (MonGroup, bool)

	// GetMonGroups returns all monitoring groups under the class
	GetMonGroups() []MonGroup
}

// ResctrlGroup is the generic interface for resctrl CTRL and MON groups
type ResctrlGroup interface {
	// Name returns the name of the group
	Name() string

	// GetPids returns the process ids assigned to the group
	GetPids() ([]string, error)

	// AddPids assigns the given process ids to the group
	AddPids(pids ...string) error

	// GetMonData retrieves the monitoring data of the group
	GetMonData() MonData
}

// MonGroup represents the interface to a RDT monitoring group
type MonGroup interface {
	ResctrlGroup

	// Parent returns the CtrlGroup under which the monitoring group exists
	Parent() CtrlGroup

	// GetAnnotations returns the annotations stored to the monitoring group
	GetAnnotations() map[string]string
}

// MonData contains monitoring stats of one monitoring group
type MonData struct {
	L3 MonL3Data
}

// MonL3Data contains L3 monitoring stats of one monitoring group
type MonL3Data map[uint64]MonLeafData

// MonLeafData represents the raw numerical stats from one RDT monitor data leaf
type MonLeafData map[string]uint64

// MonResource is the type of RDT monitoring resource
type MonResource string

const (
	// MonResourceL3 is the RDT L3 cache monitor resource
	MonResourceL3 MonResource = "l3"
)

type ctrlGroup struct {
	resctrlGroup

	monGroups map[string]*monGroup
}

type monGroup struct {
	resctrlGroup

	annotations map[string]string
}

type resctrlGroup struct {
	prefix string
	name   string
	parent *ctrlGroup // parent for MON groups
}

// SetLogger sets the logger instance to be used by the package. This function
// may be called even before Initialize().
func SetLogger(l Logger) {
	log = l
}

// Initialize discovers RDT support and initializes the  rdtControl singleton interface
// NOTE: should only be called once in order to avoid adding multiple notifiers
// TODO: support make multiple initializations, allowing e.g. "hot-plug" when
// 		 resctrl filesystem is mounted
func Initialize(resctrlGroupPrefix string, conf *Config) error {
	var err error

	rdt = &control{Logger: log, resctrlGroupPrefix: resctrlGroupPrefix}

	// Get info from the resctrl filesystem
	rdt.info, err = getRdtInfo()
	if err != nil {
		return err
	}

	// Configure resctrl
	if err = rdt.setConfig(conf); err != nil {
		return rdtError("configuration failed: %v", err)
	}

	return nil
}

// SetConfig parses new configuration and reconfigures the resctrl filesystem
// accordingly
func SetConfig(c *Config) error {
	return rdt.setConfig(c)
}

// GetClass returns one RDT class
func GetClass(name string) (CtrlGroup, bool) {
	return rdt.getClass(name)
}

// GetClasses returns all available RDT classes
func GetClasses() []CtrlGroup {
	return rdt.getClasses()
}

// MonSupported returns true if RDT monitoring features are available
func MonSupported() bool {
	return rdt.monSupported()
}

// GetMonFeatures returns the available monitoring stats of each available monitoring technology
func GetMonFeatures() map[MonResource][]string {
	return rdt.getMonFeatures()
}

func (c *control) getClass(name string) (CtrlGroup, bool) {
	cls, ok := c.classes[name]
	return cls, ok
}

func (c *control) getClasses() []CtrlGroup {
	ret := make([]CtrlGroup, 0, len(c.classes))

	for _, v := range c.classes {
		ret = append(ret, v)
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Name() < ret[j].Name() })

	return ret
}

func (c *control) monSupported() bool {
	return c.info.l3mon.Supported()
}

func (c *control) getMonFeatures() map[MonResource][]string {
	ret := make(map[MonResource][]string)
	if c.info.l3mon.Supported() {
		ret[MonResourceL3] = append([]string{}, c.info.l3mon.monFeatures...)
	}

	return ret
}

func (c *control) setConfig(newConfig *Config) error {
	c.Info("configuration update")

	conf, err := (*newConfig).resolve()
	if err != nil {
		return rdtError("invalid configuration: %v", err)
	}

	err = c.configureResctrl(conf)
	if err != nil {
		return rdtError("resctrl configuration failed: %v", err)
	}

	c.conf = conf
	// TODO: we'd better create a deep copy
	c.rawConf = *newConfig
	c.Info("configuration finished")

	return nil
}

func (c *control) configureResctrl(conf config) error {
	c.DebugBlock("  applying ", "%s", utils.DumpJSON(conf))

	// Remove stale resctrl groups
	existingClasses, err := c.classesFromResctrlFs()
	if err != nil {
		return err
	}

	for _, cls := range existingClasses {
		if _, ok := conf.Classes[cls.name]; !ok {
			tasks, err := cls.GetPids()
			if err != nil {
				return rdtError("failed to get resctrl group tasks: %v", err)
			}
			if len(tasks) > 0 {
				return rdtError("refusing to remove non-empty resctrl group %q", cls.relPath(""))
			}
			err = os.Remove(cls.path(""))
			if err != nil {
				return rdtError("failed to remove resctrl group %q: %v", cls.relPath(""), err)
			}
		}
	}

	// Start with fresh set of classes. Root class is always present
	c.classes = make(map[string]*ctrlGroup, len(conf.Classes))
	c.classes[RootClassName], err = newCtrlGroup(c.resctrlGroupPrefix, RootClassName)
	if err != nil {
		return err
	}

	// Try to apply given configuration
	for name, class := range conf.Classes {
		cg, err := newCtrlGroup(c.resctrlGroupPrefix, name)
		if err != nil {
			return err
		}

		partition := conf.Partitions[class.Partition]
		if err := cg.configure(name, class, partition, conf.Options); err != nil {
			return err
		}

		c.classes[name] = cg
	}

	return nil
}

func (c *control) classesFromResctrlFs() ([]ctrlGroup, error) {
	r, err := resctrlGroupsFromFs(c.resctrlGroupPrefix, c.info.resctrlPath)
	if err != nil {
		return nil, err
	}
	classes := make([]ctrlGroup, len(r))
	for i, grp := range r {
		classes[i] = ctrlGroup{resctrlGroup: grp}
	}
	return classes, nil
}

func (c *control) readRdtFile(rdtPath string) ([]byte, error) {
	return ioutil.ReadFile(filepath.Join(c.info.resctrlPath, rdtPath))
}

func (c *control) writeRdtFile(rdtPath string, data []byte) error {
	if err := ioutil.WriteFile(filepath.Join(c.info.resctrlPath, rdtPath), data, 0644); err != nil {
		return c.cmdError(err)
	}
	return nil
}

func (c *control) cmdError(origErr error) error {
	errData, readErr := c.readRdtFile(filepath.Join("info", "last_cmd_status"))
	if readErr != nil {
		return origErr
	}
	cmdStatus := strings.TrimSpace(string(errData))
	if len(cmdStatus) > 0 && cmdStatus != "ok" {
		return fmt.Errorf("%s", cmdStatus)
	}
	return origErr
}

func newCtrlGroup(prefix string, name string) (*ctrlGroup, error) {
	cg := &ctrlGroup{
		resctrlGroup: resctrlGroup{prefix: prefix, name: name},
		monGroups:    make(map[string]*monGroup),
	}

	if err := os.Mkdir(cg.path(""), 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}

	// Reomve existing goresctrl specific monitor groups
	// TODO: consider if these should be preserved and handled more intelligently
	mgs, err := cg.monGroupsFromResctrlFs()
	if err != nil {
		return nil, fmt.Errorf("error when retrieving existing monitor groups: %v", err)
	}
	for _, mg := range mgs {
		if err := os.Remove(mg.path("")); err != nil {
			return nil, rdtError("failed to remove existing monitoring group %q: %v", mg.relPath(""), err)
		}
	}

	return cg, nil
}

func (c *ctrlGroup) CreateMonGroup(name string, annotations map[string]string) (MonGroup, error) {
	if mg, ok := c.monGroups[name]; ok {
		return mg, nil
	}

	log.Debug("creating monitoring group %s/%s", c.name, name)
	mg, err := newMonGroup(c.prefix, name, c, annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to create new monitoring group %q: %v", name, err)
	}

	c.monGroups[name] = mg

	return mg, err
}

func (c *ctrlGroup) DeleteMonGroup(name string) error {
	mg, ok := c.monGroups[name]
	if !ok {
		log.Warn("trying to delete non-existent mon group %s/%s", c.name, name)
		return nil
	}

	log.Debug("deleting monitoring group %s/%s", c.name, name)
	if err := os.Remove(mg.path("")); err != nil {
		return rdtError("failed to remove monitoring group %q: %v", mg.relPath(""), err)
	}

	delete(c.monGroups, name)

	return nil
}

func (c *ctrlGroup) GetMonGroup(name string) (MonGroup, bool) {
	mg, ok := c.monGroups[name]
	return mg, ok
}

func (c *ctrlGroup) GetMonGroups() []MonGroup {
	ret := make([]MonGroup, 0, len(c.monGroups))

	for _, v := range c.monGroups {
		ret = append(ret, v)
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Name() < ret[j].Name() })

	return ret
}

func (c *ctrlGroup) configure(name string, class classConfig,
	partition partitionConfig, options schemaOptions) error {
	schemata := ""

	// Handle L3 cache allocation
	switch {
	case rdt.info.l3.Supported():
		schema, err := class.L3Schema.ToStr(l3SchemaTypeUnified, partition.L3)
		if err != nil {
			return err
		}
		schemata += schema
	case rdt.info.l3data.Supported() || rdt.info.l3code.Supported():
		schema, err := class.L3Schema.ToStr(l3SchemaTypeCode, partition.L3)
		if err != nil {
			return err
		}
		schemata += schema

		schema, err = class.L3Schema.ToStr(l3SchemaTypeData, partition.L3)
		if err != nil {
			return err
		}
		schemata += schema
	default:
		if class.L3Schema != nil && !options.L3.Optional {
			return rdtError("L3 cache allocation for %q specified in configuration but not supported by system", name)
		}
	}

	// Handle memory bandwidth allocation
	switch {
	case rdt.info.mb.Supported():
		schemata += class.MBSchema.ToStr(partition.MB)
	default:
		if class.MBSchema != nil && !options.MB.Optional {
			return rdtError("memory bandwidth allocation specified in configuration but not supported by system")
		}
	}

	if len(schemata) > 0 {
		log.Debug("writing schemata %q to %q", schemata, c.relPath(""))
		if err := rdt.writeRdtFile(c.relPath("schemata"), []byte(schemata)); err != nil {
			return err
		}
	} else {
		log.Debug("empty schemata")
	}

	return nil
}

func (c *ctrlGroup) monGroupsFromResctrlFs() ([]*monGroup, error) {
	r, err := resctrlGroupsFromFs(c.prefix, c.path("mon_groups"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	grps := make([]*monGroup, len(r))
	for i, grp := range r {
		grp.parent = c
		grps[i] = &monGroup{resctrlGroup: grp}
	}
	return grps, nil
}

func (r *resctrlGroup) Name() string {
	return r.name
}

func (r *resctrlGroup) GetPids() ([]string, error) {
	data, err := rdt.readRdtFile(r.relPath("tasks"))
	if err != nil {
		return []string{}, err
	}
	split := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(split[0]) > 0 {
		return split, nil
	}
	return []string{}, nil
}

func (r *resctrlGroup) AddPids(pids ...string) error {
	f, err := os.OpenFile(r.path("tasks"), os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, pid := range pids {
		if _, err := f.WriteString(pid + "\n"); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				log.Debug("no task %s", pid)
			} else {
				return rdtError("failed to assign processes %v to class %q: %v", pids, r.name, rdt.cmdError(err))
			}
		}
	}
	return nil
}

func (r *resctrlGroup) GetMonData() MonData {
	m := MonData{}

	if rdt.info.l3mon.Supported() {
		l3, err := r.getMonL3Data()
		if err != nil {
			log.Warn("failed to retrieve L3 monitoring data: %v", err)
		} else {
			m.L3 = l3
		}
	}

	return m
}

func (r *resctrlGroup) getMonL3Data() (MonL3Data, error) {
	files, err := ioutil.ReadDir(r.path("mon_data"))
	if err != nil {
		return nil, err
	}

	m := MonL3Data{}
	for _, file := range files {
		name := file.Name()
		if strings.HasPrefix(name, "mon_L3_") {
			// Parse cache id from the dirname
			id, err := strconv.ParseUint(strings.TrimPrefix(name, "mon_L3_"), 10, 32)
			if err != nil {
				// Just print a warning, we try to retrieve as much info as possible
				log.Warn("error parsing L3 monitor data directory name %q: %v", name, err)
				continue
			}

			data, err := r.getMonLeafData(filepath.Join("mon_data", name))
			if err != nil {
				log.Warn("failed to read monitor data: %v", err)
				continue
			}

			m[id] = data
		}
	}

	return m, nil
}

func (r *resctrlGroup) getMonLeafData(path string) (MonLeafData, error) {
	files, err := ioutil.ReadDir(r.path(path))
	if err != nil {
		return nil, err
	}

	m := make(MonLeafData, len(files))

	for _, file := range files {
		name := file.Name()

		// We expect that all the files in the dir are regular files
		val, err := readFileUint64(r.path(path, name))
		if err != nil {
			// Just print a warning, we want to retrieve as much info as possible
			log.Warn("error reading data file: %v", err)
			continue
		}

		m[name] = val
	}
	return m, nil
}

func (r *resctrlGroup) relPath(elem ...string) string {
	if r.parent == nil {
		if r.name == RootClassName {
			return filepath.Join(elem...)
		}
		return filepath.Join(append([]string{r.prefix + r.name}, elem...)...)
	}
	// Parent is only intended for MON groups - non-root CTRL groups are considered
	// as peers to the root CTRL group (as they are in HW) and do not have a parent
	return r.parent.relPath(append([]string{"mon_groups", r.prefix + r.name}, elem...)...)
}

func (r *resctrlGroup) path(elem ...string) string {
	return filepath.Join(rdt.info.resctrlPath, r.relPath(elem...))
}

func newMonGroup(prefix string, name string, parent *ctrlGroup, annotations map[string]string) (*monGroup, error) {
	mg := &monGroup{
		resctrlGroup: resctrlGroup{prefix: prefix, name: name, parent: parent},
		annotations:  make(map[string]string, len(annotations))}

	if err := os.Mkdir(mg.path(""), 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}
	for k, v := range annotations {
		mg.annotations[k] = v
	}

	return mg, nil
}

func (m *monGroup) Parent() CtrlGroup {
	return m.parent
}

func (m *monGroup) GetAnnotations() map[string]string {
	a := make(map[string]string, len(m.annotations))
	for k, v := range m.annotations {
		a[k] = v
	}
	return a
}

func resctrlGroupsFromFs(prefix string, path string) ([]resctrlGroup, error) {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	grps := make([]resctrlGroup, 0, len(files))
	for _, file := range files {
		filename := file.Name()
		if strings.HasPrefix(filename, prefix) {
			grps = append(grps,
				resctrlGroup{
					prefix: prefix,
					name:   filename[len(prefix):],
				})
		}
	}
	return grps, nil
}

func rdtError(format string, args ...interface{}) error {
	return fmt.Errorf("rdt: "+format, args...)
}
