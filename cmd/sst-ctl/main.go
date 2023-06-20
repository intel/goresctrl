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
	"strconv"
	"strings"

	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/sst"
	"github.com/intel/goresctrl/pkg/utils"
)

var (
	// Global command line flags
	packageIds string
)

type subCmd func([]string) error

var subCmds = map[string]subCmd{
	"info": subCmdInfo,
	"bf":   subCmdBF,
	"cp":   subCmdCP,
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
	flagset.StringVar(&packageIds, "package", "", "One or more physical package id")
	flagset.Func("prefix", "set mount prefix for system directories", func(s string) error {
		goresctrlpath.SetPrefix(s)
		return nil
	})
}

func printPackageInfo(pkgId ...int) error {
	info, err := sst.GetPackageInfo(pkgId...)
	if err != nil {
		return err
	}

	fmt.Println(utils.DumpJSON(info))

	return nil
}

// TODO: Move this functionality into utils.NewIdSetFromString()
func str2slice(str string) []int {
	var s []int

	for _, str := range strings.Split(str, ",") {
		if str == "" {
			continue
		}

		id, err := strconv.ParseUint(str, 10, 0)
		if err != nil {
			fmt.Printf("invalid value '%s': %v", str, err)
			continue
		}

		s = append(s, int(id))
	}

	return s
}

func subCmdInfo(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("info", flag.ExitOnError)
	addGlobalFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}

	return printPackageInfo(str2slice(packageIds)...)
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

	pkgs := str2slice(packageIds)

	if enable {
		err = enableBF(pkgs...)
	} else {
		err = disableBF(pkgs...)
	}

	return err
}

func getPackage(packageStr string, cpus utils.IDSet) (map[int]*sst.SstPackageInfo, *sst.SstPackageInfo, []int, error) {
	var infomap map[int]*sst.SstPackageInfo
	var info *sst.SstPackageInfo
	var packageId int
	var err error

	// If user has specified a package, then all the CPUs must belong to it.
	pkgs := str2slice(packageStr)
	if len(pkgs) > 1 {
		fmt.Printf("Only one package can be configured at a time (you have %d)\n", len(pkgs))
		return nil, nil, nil, fmt.Errorf("Provide one package value only")
	}

	if len(pkgs) == 0 {
		// User has not specified a package, figure it out from the
		// first CPU in the list.
		infomap, err = sst.GetPackageInfo()
		if err != nil {
			return nil, nil, nil, err
		}

		for packageId, info = range infomap {
			if sst.CheckPackageCpus(info, cpus) {
				pkgs = append(pkgs, packageId)
				break
			}
		}
	} else {
		// User has specified one package, make sure all the CPUs belong to it.
		infomap, err = sst.GetPackageInfo(pkgs...)
		if err != nil {
			return nil, nil, nil, err
		}

		for packageId, info = range infomap {
			if !sst.CheckPackageCpus(info, cpus) {
				fmt.Printf("All the CPUs %v must belong to one specific package\n", cpus)
				return nil, nil, nil, fmt.Errorf("Not all CPUs belong to package %d", packageId)
			}

			pkgs = append(pkgs, packageId)
			break
		}
	}

	return infomap, info, pkgs, nil
}

// TODO: Instead of all CP parameters groupped together, separate them like this.
//    sst-ctl cp enable
//    sst-ctl cp disable
//    sst-ctl cp configure -clos=CLOS...
//    sst-ctl cp assign...

func subCmdCP(args []string) error {
	var enable, disable, reset bool

	// Clos setup variables
	var epp, minFreq, maxFreq, desiredFreq, proportionalPriority, clos int

	// CPU to Clos mapping variables
	var cpuStr string
	var cpus utils.IDSet

	var packageId int
	var pkgs []int
	var info *sst.SstPackageInfo
	var infomap map[int]*sst.SstPackageInfo
	var err error
	var priority int

	defaultMaxFreq := 0xff

	flags := flag.NewFlagSet("cp", flag.ExitOnError)
	flags.BoolVar(&enable, "enable", false, "Enable feature")
	flags.BoolVar(&disable, "disable", false, "Disable feature")
	flags.BoolVar(&reset, "reset", false, "Reset CP to a known state")
	flags.IntVar(&clos, "clos", -1, "Class of service (0 - 3)")
	flags.IntVar(&epp, "epp", 0, "Energy Performance Preference value, Lower value favors performance, and higher value favors power. The value can be between 0 and 15. The default value is 0.")
	flags.IntVar(&minFreq, "min", 0, "Clos minimum frequency MHz")
	flags.IntVar(&maxFreq, "max", defaultMaxFreq, "Clos maximum frequency MHz")
	flags.IntVar(&desiredFreq, "desired", 0, "Clos desired frequency MHz")
	flags.IntVar(&proportionalPriority, "proportional", 0, "Clos proportional priority weight. Used if CP priority mode is 0 (Proportional)")
	flags.IntVar(&priority, "priority", 1, "CP priority mode. 0 is Proportional, 1 is Ordered.")
	flags.StringVar(&cpuStr, "cpus", "", "List of CPUs assigned to the Clos.")

	addGlobalFlags(flags)

	flags.Usage = func() {
		flags.PrintDefaults()

		fmt.Fprintf(os.Stderr, "\nExample usage:\n\n")
		fmt.Fprintf(os.Stderr, "First reset CP to default:\n\t%s cp -reset\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Then set the CLOS values:\n\t%s cp -clos 1 -desired 280 -epp 1 -max 30 -min 21 -priority 1 -package 0\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Then bind CPUs to a CLOS:\n\t%s cp -clos 1 -cpus 1,3,5,6\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Finally enable CP:\n\t%s cp -enable -package 0\n\n", os.Args[0])
	}

	if err := flags.Parse(args); err != nil {
		return err
	}

	if reset {
		err := sst.ResetCPConfig()
		_ = printPackageInfo()
		return err
	}

	if enable && disable {
		return fmt.Errorf("Please provide either -enable or -disable flag")
	}

	// If user specifies a list of CPUs, then he/she wants to assign those
	// CPUs to a specific CLOS. If the -cpus option is not set, then user
	// wants to configure the actual CLOS values. Both operations cannot be
	// done at the same time.

	// If user specifies a list of CPUs, then the package option is ignored.
	// Verify that all the CPUs belong to one specific package.
	if cpuStr != "" {
		cpus = utils.NewIDSet(str2slice(cpuStr)...)

		infomap, info, pkgs, err = getPackage(packageIds, cpus)
		if err != nil {
			return fmt.Errorf("Cannot get CPUs %v package: %w", cpus, err)
		}

		if len(pkgs) == 0 {
			return fmt.Errorf("All the CPUs %v must belong to one specific package", cpus)
		}

		if clos < 0 {
			return fmt.Errorf("Clos not set, use -clos option")
		}

		cpu2Clos := make(sst.ClosCPUSet, 1)
		cpu2Clos[clos] = cpus

		if err := sst.ConfigureCP(info, priority, &cpu2Clos); err != nil {
			return err
		}

	} else if clos >= 0 {
		pkgs = str2slice(packageIds)
		if len(pkgs) == 0 {
			return fmt.Errorf("No packages set, invalid value %q", packageIds)
		}

		closinfo := sst.SstClosInfo{
			EPP:                  epp,
			ProportionalPriority: proportionalPriority,
			MinFreq:              minFreq,
			MaxFreq:              maxFreq,
			DesiredFreq:          desiredFreq,
		}

		infomap, err = sst.GetPackageInfo(pkgs...)
		if err != nil {
			return fmt.Errorf("Cannot get package info: %w", err)
		}

		for _, info = range infomap {
			if err := sst.ClosSetup(info, clos, &closinfo); err != nil {
				return fmt.Errorf("Cannot set Clos: %w", err)
			}
		}
	} else {
		if (!enable && !disable) && clos < 0 {
			return fmt.Errorf("Clos not set, use -clos option")
		}

		// Print information if user just wants to enable / disable CP
		infomap, _ = sst.GetPackageInfo(pkgs...)
	}

	if enable || disable {
		for packageId, info = range infomap {
			if enable {
				fmt.Printf("Enabling CP for package %d\n", packageId)

				err = sst.EnableCP(info)
				if err != nil {
					return err
				}
			} else if disable {
				fmt.Printf("Disabling CP for package %d\n", packageId)

				err = sst.DisableCP(info)
				if err != nil {
					return err
				}
			}
		}
	}

	for packageId = range infomap {
		// If we add a CPU to Clos, punit might add another CPU to same Clos.
		// Make sure we have re-read the package info before printing it
		_ = printPackageInfo(packageId)
	}

	return nil
}
