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

// This application demonstrates using the cstates API
package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/intel/goresctrl/pkg/cstates"
	"github.com/intel/goresctrl/pkg/utils"
)

type subCmd struct {
	description string
	f           func([]string) error
}

var subCmds = map[string]subCmd{
	"help": subCmd{
		description: "Display this help",
		f:           subCmdHelp,
	},
	"ls": subCmd{
		description: "List C-states and their attributes",
		f:           subCmdLs,
	},
	"set": subCmd{
		description: "Set C-state attributes",
		f:           subCmdSet,
	},
}

// nolint:errcheck
func usage() {
	f := flag.CommandLine.Output()
	fmt.Fprint(f, `Usage: cstates <command> [options]

Available commands:`)

	for _, c := range slices.Sorted(maps.Keys(subCmds)) {
		fmt.Fprintf(f, "\n  %-12s %s", c, subCmds[c].description)
	}

	fmt.Fprint(f, `

Use "cstates <command> --help" for more information about a command.
`)
}

func subCmdHelp(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("help", flag.ExitOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Run sub-command
	flag.Usage()
	return nil
}

func subCmdLs(args []string) error {
	var filter cstates.BasicFilter
	var optCpus string
	var optListAbove bool
	var optListBelow bool
	var optListDisable bool
	var optListLatency bool
	var optListResidency bool
	var optListTime bool
	var optNames string
	var optSummarizeDisabled bool
	var optSummarizeEnabled bool
	var optListAll bool
	var readAttrs = []cstates.AttrID{}

	// Parse command line args
	flags := flag.NewFlagSet("ls", flag.ExitOnError)
	flags.BoolVar(&optListAll, "A", false, "List all C-state attributes")
	flags.BoolVar(&optListAbove, "a", false, "List C-state above")
	flags.BoolVar(&optListBelow, "b", false, "List C-state below")
	flags.StringVar(&optCpus, "c", "", "Comma-separated list of CPU IDs or ranges to operate on (default: all CPUs)")
	flags.BoolVar(&optListDisable, "d", false, "List C-state disable")
	flags.BoolVar(&optListLatency, "l", false, "List C-state latency")
	flags.BoolVar(&optListResidency, "r", false, "List C-state residency")
	flags.BoolVar(&optListTime, "t", false, "List C-state time")
	flags.StringVar(&optNames, "n", "", "Comma-separated list of C-state names to include (default: all)")
	flags.BoolVar(&optSummarizeDisabled, "D", false, "Summarize CPUs where a state is disabled")
	flags.BoolVar(&optSummarizeEnabled, "E", false, "Summarize CPUs where a state is enabled")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if optCpus != "" {
		cpus, err := utils.NewIDSetFromString(optCpus)
		if err != nil {
			return fmt.Errorf("invalid CPU list %q: %v", optCpus, err)
		}
		filter.SetCPUs(cpus.Members()...)
	}
	if optNames != "" {
		filter.SetCstateNames(strings.Split(optNames, ",")...)
	}
	if optListAll {
		optListAbove = true
		optListBelow = true
		optListDisable = true
		optListLatency = true
		optListResidency = true
		optListTime = true
	}
	if optListAbove {
		readAttrs = append(readAttrs, cstates.AttrAbove)
	}
	if optListBelow {
		readAttrs = append(readAttrs, cstates.AttrBelow)
	}
	if optSummarizeDisabled || optSummarizeEnabled || optListDisable {
		readAttrs = append(readAttrs, cstates.AttrDisable)
	}
	if optListTime {
		readAttrs = append(readAttrs, cstates.AttrTime)
	}
	if optListResidency {
		readAttrs = append(readAttrs, cstates.AttrResidency)
	}
	if optListLatency {
		readAttrs = append(readAttrs, cstates.AttrLatency)
	}
	if len(readAttrs) > 0 {
		filter.SetAttributes(readAttrs...)
	}

	// Create a new cstates controller
	cs, err := cstates.NewCstatesFromSysfs(&filter)
	if err != nil {
		return fmt.Errorf("error creating cstates controller: %v", err)
	}

	// List available C-states
	names := cs.Names()
	for _, name := range names {
		printed := false
		csName := cs.Copy(cstates.NewBasicFilter().SetCstateNames(name))
		if optSummarizeDisabled {
			csDisabled := csName.Copy(cstates.NewBasicFilter().SetAttributeValues(cstates.AttrDisable, "1"))
			fmt.Printf("%s disabled: %s\n", name, csDisabled.CPUs())
			printed = true
		}
		if optSummarizeEnabled {
			csEnabled := csName.Copy(cstates.NewBasicFilter().SetAttributeValues(cstates.AttrDisable, "0"))
			fmt.Printf("%s enabled: %s\n", name, csEnabled.CPUs())
			printed = true
		}
		for _, cpu := range csName.CPUs().SortedMembers() {
			optAttrDescr := []struct {
				opt  bool
				attr cstates.AttrID
				name string
			}{
				{optListAbove, cstates.AttrAbove, "above"},
				{optListBelow, cstates.AttrBelow, "below"},
				{optListDisable, cstates.AttrDisable, "disable"},
				{optListTime, cstates.AttrTime, "time"},
				{optListLatency, cstates.AttrLatency, "latency"},
				{optListResidency, cstates.AttrResidency, "residency"},
			}

			for _, oad := range optAttrDescr {
				if !oad.opt {
					continue
				}
				val := "<error: not found>"
				if valp := csName.GetAttr(cpu, name, oad.attr); valp != nil {
					val = *valp
				}
				fmt.Printf("%s cpu%d %s: %s\n", name, cpu, oad.name, val)
				printed = true
			}
		}
		if !printed {
			fmt.Printf("%s\n", name)
		}
	}
	return nil
}

func subCmdSet(args []string) error {
	var filter cstates.BasicFilter
	var optCpus string
	var optDisable bool
	var optEnable bool
	var optNames string

	// Parse command line args
	flags := flag.NewFlagSet("set", flag.ExitOnError)
	flags.StringVar(&optCpus, "c", "", "Comma-separated list of CPU IDs or ranges to operate on (default: all CPUs)")
	flags.StringVar(&optNames, "n", "", "Comma-separated list of C-state names to include (default: all)")
	flags.BoolVar(&optDisable, "d", false, "Disable C-state")
	flags.BoolVar(&optEnable, "e", false, "Enable C-state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if optCpus != "" {
		cpus, err := utils.NewIDSetFromString(optCpus)
		if err != nil {
			return fmt.Errorf("invalid CPU list %q: %v", optCpus, err)
		}
		filter.SetCPUs(cpus.Members()...)
	}
	if optNames != "" {
		filter.SetCstateNames(strings.Split(optNames, ",")...)
	}
	if optDisable || optEnable {
		filter.SetAttributes(cstates.AttrDisable)
		cs, err := cstates.NewCstatesFromSysfs(&filter)
		if err != nil {
			return fmt.Errorf("error reading cstates: %w", err)
		}
		val := "0"
		if optDisable {
			val = "1"
		}
		cs.SetAttrs(cstates.AttrDisable, val)
		if err := cs.Apply(); err != nil {
			return fmt.Errorf("error applying cstate changes: %w", err)
		}
	}
	return nil
}

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = usage

	// Parse global command line flags
	help := flag.Bool("help", false, "Display this help")
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	// Run sub-command
	cmd, ok := subCmds[args[0]]
	if !ok {
		fmt.Printf("unknown sub-command %q\n", args[0])
		flag.Usage()
		os.Exit(2)
	}

	if err := cmd.f(args[1:]); err != nil {
		fmt.Printf("sub-command %q failed: %v\n", args[0], err)
		os.Exit(1)
	}
}
