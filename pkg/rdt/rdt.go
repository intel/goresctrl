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

// Package rdt implements an API for managing Intel® RDT technologies via the
// resctrl pseudo-filesystem of the Linux kernel. It provides flexible
// configuration with a hierarchical approach for easy management of exclusive
// cache allocations.
//
// Goresctrl supports all available RDT technologies, i.e. L2 and L3 Cache
// Allocation (CAT) with Code and Data Prioritization (CDP) and Memory
// Bandwidth Allocation (MBA) plus Cache Monitoring (CMT) and Memory Bandwidth
// Monitoring (MBM).
//
// Basic usage example:
//
//	rdt.SetLogger(logrus.New())
//
//	if err := rdt.Initialize(""); err != nil {
//		return fmt.Errorf("RDT not supported: %v", err)
//	}
//
//	if err := rdt.SetConfigFromFile("/path/to/rdt.conf.yaml", false); err != nil {
//		return fmt.Errorf("RDT configuration failed: %v", err)
//	}
//
//	if cls, ok := rdt.GetClass("my-class"); ok {
//	   //  Set PIDs 12345 and 12346 to class "my-class"
//		if err := cls.AddPids("12345", "12346"); err != nil {
//			return fmt.Errorf("failed to add PIDs to RDT class: %v", err)
//		}
//	}
package rdt

import (
	"fmt"
	stdlog "log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	grclog "github.com/intel/goresctrl/pkg/log"
	"github.com/intel/goresctrl/pkg/utils"
)

const (
	// RootClassName is the name we use in our config for the special class
	// that configures the "root" resctrl group of the system
	RootClassName = "system/default"
	// RootClassAlias is an alternative name for the root class
	RootClassAlias = ""
)

type control struct {
	grclog.Logger

	resctrlGroupPrefix string
	conf               config
	rawConf            Config
	classes            map[string]*ctrlGroup
}

var log grclog.Logger = grclog.NewLoggerWrapper(stdlog.New(os.Stderr, "[ rdt ] ", 0))

var info *resctrlInfo

var rdt *control

// SetLogger sets the logger instance to be used by the package. This function
// may be called even before Initialize().
func SetLogger(l grclog.Logger) {
	log = l
	if rdt != nil {
		rdt.setLogger(l)
	}
}

// Initialize detects RDT from the system and initializes control interface of
// the package.
func Initialize(resctrlGroupPrefix string) error {
	var err error

	info = nil
	rdt = nil

	// Get info from the resctrl filesystem
	info, err = getRdtInfo()
	if err != nil {
		return err
	}

	r := &control{Logger: log, resctrlGroupPrefix: resctrlGroupPrefix}

	// NOTE: we lose monitoring group annotations (i.e. prometheus metrics
	// labels) on re-init
	if r.classes, err = r.classesFromResctrlFs(); err != nil {
		return fmt.Errorf("failed to initialize classes from resctrl fs: %v", err)
	}

	rdt = r

	return nil
}

// DiscoverClasses discovers existing classes from the resctrl filesystem.
// Makes it possible to discover gropus with another prefix than was set with
// Initialize(). The original prefix is still used for monitoring groups.
func DiscoverClasses(resctrlGroupPrefix string) error {
	if rdt != nil {
		return rdt.discoverFromResctrl(resctrlGroupPrefix)
	}
	return fmt.Errorf("rdt not initialized")
}

// SetConfig  (re-)configures the resctrl filesystem according to the specified
// configuration.
func SetConfig(c *Config, force bool) error {
	if rdt != nil {
		return rdt.setConfig(c, force)
	}
	return fmt.Errorf("rdt not initialized")
}

// SetConfigFromData takes configuration as raw data, parses it and
// reconfigures the resctrl filesystem.
func SetConfigFromData(data []byte, force bool) error {
	cfg := &Config{}
	if err := yaml.UnmarshalStrict(data, cfg); err != nil {
		return fmt.Errorf("failed to parse configuration data: %v", err)
	}

	return SetConfig(cfg, force)
}

// SetConfigFromFile reads configuration from the filesystem and reconfigures
// the resctrl filesystem.
func SetConfigFromFile(path string, force bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	if err := SetConfigFromData(data, force); err != nil {
		return err
	}

	log.Infof("configuration successfully loaded from %q", path)
	return nil
}

// GetClass returns one RDT class.
func GetClass(name string) (CtrlGroup, bool) {
	if rdt != nil {
		return rdt.getClass(name)
	}
	return nil, false
}

// GetClasses returns all available RDT classes.
func GetClasses() []CtrlGroup {
	if rdt != nil {
		return rdt.getClasses()
	}
	return []CtrlGroup{}
}

// MonSupported returns true if RDT monitoring features are available.
func MonSupported() bool {
	if rdt != nil {
		return rdt.monSupported()
	}
	return false
}

// GetMonFeatures returns the available monitoring stats of each available
// monitoring technology.
func GetMonFeatures() map[MonResource][]string {
	if rdt != nil {
		return rdt.getMonFeatures()
	}
	return map[MonResource][]string{}
}

// IsQualifiedClassName returns true if given string qualifies as a class name
func IsQualifiedClassName(name string) bool {
	// Must be qualified as a file name
	return name == RootClassName || (len(name) < 4096 && name != "." && name != ".." && !strings.ContainsAny(name, "/\n"))
}

func (c *control) getClass(name string) (CtrlGroup, bool) {
	cls, ok := c.classes[unaliasClassName(name)]
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
	return info.l3mon.Supported()
}

func (c *control) getMonFeatures() map[MonResource][]string {
	ret := make(map[MonResource][]string)
	if info.l3mon.Supported() {
		ret[MonResourceL3] = append([]string{}, info.l3mon.monFeatures...)
	}

	return ret
}

func (c *control) setLogger(l grclog.Logger) {
	c.Logger = l
}

func (c *control) setConfig(newConfig *Config, force bool) error {
	c.Infof("configuration update")

	conf, err := (*newConfig).resolve()
	if err != nil {
		return fmt.Errorf("invalid configuration: %v", err)
	}

	err = c.configureResctrl(conf, force)
	if err != nil {
		return fmt.Errorf("resctrl configuration failed: %v", err)
	}

	c.conf = conf
	// TODO: we'd better create a deep copy
	c.rawConf = *newConfig
	c.Infof("configuration finished")

	return nil
}

func (c *control) configureResctrl(conf config, force bool) error {
	grclog.DebugBlock(c, "applying resolved config:", "  ", "%s", utils.DumpJSON(conf))

	// Remove stale resctrl groups
	classesFromFs, err := c.classesFromResctrlFs()
	if err != nil {
		return err
	}

	for name, cls := range classesFromFs {
		if _, ok := conf.Classes[cls.name]; !isRootClass(cls.name) && !ok {
			if !force {
				tasks, err := cls.GetPids()
				if err != nil {
					return fmt.Errorf("failed to get resctrl group tasks: %v", err)
				}
				if len(tasks) > 0 {
					return fmt.Errorf("refusing to remove non-empty resctrl group %q", cls.relPath(""))
				}
			}
			log.Debugf("removing existing resctrl group %q", cls.relPath(""))
			err = groupRemoveFunc(cls.path(""))
			if err != nil {
				return fmt.Errorf("failed to remove resctrl group %q: %v", cls.relPath(""), err)
			}

			delete(c.classes, name)
		}
	}

	for name, cls := range c.classes {
		if _, ok := conf.Classes[cls.name]; !ok || cls.prefix != c.resctrlGroupPrefix {
			if !isRootClass(cls.name) {
				log.Debugf("dropping stale class %q (%q)", name, cls.path(""))
				delete(c.classes, name)
			}
		}
	}

	if _, ok := c.classes[RootClassName]; !ok {
		log.Warnf("root class missing from runtime data, re-adding...")
		c.classes[RootClassName] = classesFromFs[RootClassName]
	}

	// Try to apply given configuration
	for name, class := range conf.Classes {
		if _, ok := c.classes[name]; !ok {
			cg, err := newCtrlGroup(c.resctrlGroupPrefix, c.resctrlGroupPrefix, name)
			if err != nil {
				return err
			}
			c.classes[name] = cg
		}
		partition := conf.Partitions[class.Partition]
		if err := c.classes[name].configure(name, class, partition, conf.Options); err != nil {
			return err
		}
	}

	if err := c.pruneMonGroups(); err != nil {
		return err
	}

	return nil
}

func (c *control) discoverFromResctrl(prefix string) error {
	c.Debugf("running class discovery from resctrl filesystem using prefix %q", prefix)

	classesFromFs, err := c.classesFromResctrlFsPrefix(prefix)
	if err != nil {
		return err
	}

	// Drop stale classes
	for name, cls := range c.classes {
		if _, ok := classesFromFs[cls.name]; !ok || cls.prefix != prefix {
			if !isRootClass(cls.name) {
				log.Debugf("dropping stale class %q (%q)", name, cls.path(""))
				delete(c.classes, name)
			}
		}
	}

	for name, cls := range classesFromFs {
		if _, ok := c.classes[name]; !ok {
			c.classes[name] = cls
			log.Debugf("adding discovered class %q (%q)", name, cls.path(""))
		}
	}

	if err := c.pruneMonGroups(); err != nil {
		return err
	}

	return nil
}

func (c *control) classesFromResctrlFs() (map[string]*ctrlGroup, error) {
	return c.classesFromResctrlFsPrefix(c.resctrlGroupPrefix)
}

func (c *control) classesFromResctrlFsPrefix(prefix string) (map[string]*ctrlGroup, error) {
	names := []string{RootClassName}
	if g, err := resctrlGroupsFromFs(prefix, info.resctrlPath); err != nil {
		return nil, err
	} else {
		for _, n := range g {
			if prefix != c.resctrlGroupPrefix &&
				strings.HasPrefix(n, c.resctrlGroupPrefix) &&
				strings.HasPrefix(c.resctrlGroupPrefix, prefix) {
				// Skip groups in the standard namespace
				continue
			}
			names = append(names, n[len(prefix):])
		}
	}

	classes := make(map[string]*ctrlGroup, len(names)+1)
	for _, name := range names {
		g, err := newCtrlGroup(prefix, c.resctrlGroupPrefix, name)
		if err != nil {
			return nil, err
		}
		classes[name] = g
	}

	return classes, nil
}

func (c *control) pruneMonGroups() error {
	for name, cls := range c.classes {
		if err := cls.pruneMonGroups(); err != nil {
			return fmt.Errorf("failed to prune stale monitoring groups of %q: %v", name, err)
		}
	}
	return nil
}

func (c *control) readRdtFile(rdtPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(info.resctrlPath, rdtPath))
}

func (c *control) writeRdtFile(rdtPath string, data []byte) error {
	if err := os.WriteFile(filepath.Join(info.resctrlPath, rdtPath), data, 0644); err != nil {
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

func resctrlGroupsFromFs(prefix string, path string) ([]string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	grps := make([]string, 0, len(files))
	for _, file := range files {
		filename := file.Name()
		if strings.HasPrefix(filename, prefix) {
			if s, err := os.Stat(filepath.Join(path, filename, "tasks")); err == nil && !s.IsDir() {
				grps = append(grps, filename)
			}
		}
	}
	return grps, nil
}

func isRootClass(name string) bool {
	return name == RootClassName || name == RootClassAlias
}

func unaliasClassName(name string) string {
	if isRootClass(name) {
		return RootClassName
	}
	return name
}
