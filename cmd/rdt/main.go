/*
Copyright 2021 Intel Corporation

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

// This application demonstrates using the blockio API.

package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/intel/goresctrl/pkg/rdt"
)

var (
	// Global command line flags
	groupPrefix string
)

type subCmd func([]string) error

var subCmds = map[string]subCmd{
	"info":      subCmdInfo,
	"configure": subCmdConfigure,
}

func main() {
	cmds := make([]string, 0, len(subCmds))
	for c := range subCmds {
		cmds = append(cmds, c)
	}
	sort.Strings(cmds)
	allCmds := strings.Join(cmds, ", ")

	if len(os.Args) < 2 {
		exitError("missing sub-command, must be one of: %s\n", allCmds)
	}

	// Run sub-command
	cmd, ok := subCmds[os.Args[1]]
	if !ok {
		exitError("unknown sub-command %q, must be of: %s\n", os.Args[1], allCmds)
	}

	if err := cmd(os.Args[2:]); err != nil {
		exitError("sub-command %q failed: %v\n", os.Args[1], err)
	}
}

func addGlobalFlags(flagset *flag.FlagSet) {
	flagset.StringVar(&groupPrefix, "group-prefix", "", "prefix to use for resctrl groups")
}

func subCmdInfo(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("info", flag.ExitOnError)
	addGlobalFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}

	// Run sub-command
	if err := rdt.Initialize(groupPrefix); err != nil {
		fmt.Printf("RDT is not enabled: %v\n", err)
		return nil
	}

	fmt.Printf("Monitoring supported: %v\n", rdt.MonSupported())
	if rdt.MonSupported() {
		mon := rdt.GetMonFeatures()

		fmt.Println("Monitoring features:")
		for r, f := range mon {
			fmt.Printf("  - %s: %v\n", r, strings.Join(f, ", "))
		}
	}
	fmt.Println("Classes:")
	for _, cls := range rdt.GetClasses() {
		fmt.Printf("  - %s\n", cls.Name())

		mon := cls.GetMonGroups()
		if len(mon) > 0 {
			fmt.Println("    Monitoring groups:")
			for _, grp := range mon {
				fmt.Printf("      - %s\n", grp.Name())
			}
		}
	}

	return nil
}

func subCmdConfigure(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("configure", flag.ExitOnError)
	addGlobalFlags(flags)

	configFile := flags.String("config-file", "", "path to rdt configuration file")
	force := flags.Bool("force", false, "force configuration, delete non-empty resctrl groups")

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *configFile == "" {
		return fmt.Errorf("-config-file must be specified")
	}

	// Run sub-command
	if err := rdt.Initialize(groupPrefix); err != nil {
		return fmt.Errorf("RDT is not enabled: %v", err)
	}

	fmt.Println("Configuring resctrl filesystem...")
	if err := rdt.SetConfigFromFile(*configFile, *force); err != nil {
		return err
	}

	fmt.Println("Done!")

	return nil
}

func exitError(format string, args ...interface{}) {
	fmt.Printf("ERROR: "+format+"\n", args...)
	os.Exit(1)
}
