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

	"github.com/intel/goresctrl/pkg/sst"
)

func enableBF(pkgId ...int) error {
	if len(pkgId) == 0 {
		fmt.Printf("Enabling BF for all packages\n")
	} else {
		fmt.Printf("Enabling BF for package(s) %v\n", pkgId)
	}

	if err := sst.EnableBF(pkgId...); err != nil {
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

	if err := sst.DisableBF(pkgId...); err != nil {
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

	pkgs := str2slice(packageIds)

	if enable {
		return enableBF(pkgs...)
	}
	return disableBF(pkgs...)
}
