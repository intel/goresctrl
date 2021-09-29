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

package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/intel/goresctrl/pkg/sst"
	"github.com/intel/goresctrl/pkg/utils"
)

var (
	// Global command line flags
	packageId int
)

type subCmd func([]string) error

var subCmds = map[string]subCmd{
	"info": subCmdInfo,
	"bf":   subCmdBF,
}

func main() {
	cmds := make([]string, 0, len(subCmds))
	for c := range subCmds {
		cmds = append(cmds, c)
	}
	sort.Strings(cmds)
	allCmds := strings.Join(cmds, ", ")

	if len(os.Args) < 2 {
		fmt.Printf("missing sub-command, must be one of: %s\n", allCmds)
		os.Exit(1)
	}

	// Run sub-command
	cmd, ok := subCmds[os.Args[1]]
	if !ok {
		fmt.Printf("unknown sub-command %q, must be of: %s\n", os.Args[1], allCmds)
		os.Exit(1)
	}

	if err := cmd(os.Args[2:]); err != nil {
		fmt.Printf("ERROR: sub-command %q failed: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}

func addGlobalFlags(flagset *flag.FlagSet) {
	flagset.IntVar(&packageId, "package", -1, "physical package id")
}

func printPackageInfo(pkgId ...int) error {
	info, err := sst.GetPackageInfo(pkgId...)
	if err != nil {
		return err
	}

	fmt.Println(utils.DumpJSON(info))

	return nil
}

func subCmdInfo(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("info", flag.ExitOnError)
	addGlobalFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}

	if packageId < 0 {
		return printPackageInfo()
	} else {
		return printPackageInfo(packageId)
	}
}

func enableBF(pkgId ...int) error {
	if len(pkgId) == 0 {
		fmt.Printf("Enabling BF for all packages\n")
	} else {
		fmt.Printf("Enabling BF for package(s) %v\n", pkgId)
	}

	err := sst.EnableBF(pkgId...)
	if err != nil {
		return err
	}

	return printPackageInfo(pkgId...)
}

func disableBF(pkgId ...int) error {
	if len(pkgId) == 0 {
		fmt.Printf("Disabling BF for all packages\n")
	} else {
		fmt.Printf("Disabling BF for package(s) %v\n", pkgId)
	}

	err := sst.DisableBF(pkgId...)
	if err != nil {
		return err
	}

	return printPackageInfo(pkgId...)
}

func subCmdBF(args []string) error {
	var enable, disable bool

	flags := flag.NewFlagSet("bf", flag.ExitOnError)
	flags.BoolVar(&enable, "enable", false, "enable feature")
	flags.BoolVar(&disable, "disable", false, "disable feature")
	addGlobalFlags(flags)

	if err := flags.Parse(args); err != nil {
		return err
	}

	if (!enable && !disable) || (enable && disable) {
		fmt.Printf("Please provide either -enable or -disable flag\n")
		return nil
	}

	var err error

	if enable {
		if packageId < 0 {
			err = enableBF()
		} else {
			err = enableBF(packageId)
		}
	} else {
		if packageId < 0 {
			err = disableBF()
		} else {
			err = disableBF(packageId)
		}
	}

	return err
}
