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
	"log/slog"
	"maps"
	"os"
	"slices"

	"github.com/intel/goresctrl/pkg/log"
	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/sst"
)

var (
	// Global command line flags
	packageIds string
)

type subCmd struct {
	description string
	f           func([]string) error
}

var subCmds = map[string]subCmd{
	"info": {
		description: "Print SST information",
		f:           subCmdInfo,
	},
	"bf": {
		description: "Configure the SST-BF feature",
		f:           subCmdBF,
	},
	"cp": {
		description: "Configure the SST-CP feature",
		f:           subCmdCP,
	},
}

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = usage

	// Parse global command line flags
	help := flag.Bool("help", false, "Display this help")
	logLevel := log.NewLevelFlag(slog.LevelDebug)
	flag.Var(logLevel, "log-level", "Set log level (debug, info, warn, error)")
	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	sst.SetLogger(slog.New(log.NewLogHandler(logLevel)))

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

// nolint:errcheck
func usage() {
	f := flag.CommandLine.Output()
	fmt.Fprint(f, `Usage: sst-ctl [global-options] <command> [options]

Available commands:`)

	for _, c := range slices.Sorted(maps.Keys(subCmds)) {
		fmt.Fprintf(f, "\n  %-12s %s", c, subCmds[c].description)
	}

	fmt.Fprint(f, `

Use "sst-ctl <command> --help" for more information about a command.
`)

	fmt.Fprint(f, "\nGlobal options:\n")
	flag.PrintDefaults()
}

func addGlobalFlags(flagset *flag.FlagSet) {
	flagset.StringVar(&packageIds, "package", "", "One or more physical package id")
	flagset.Func("prefix", "set mount prefix for system directories", func(s string) error {
		goresctrlpath.SetPrefix(s)
		return nil
	})
}
