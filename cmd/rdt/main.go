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
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/intel/goresctrl/pkg/rdt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Global command line flags
	groupPrefix string
)

type subCmd struct {
	description string
	f           func([]string) error
}

var subCmds = map[string]subCmd{
	"configure": subCmd{
		description: "Configure resctrl filesystem",
		f:           subCmdConfigure,
	},
	"help": subCmd{
		description: "Display this help",
		f:           subCmdHelp,
	},
	"info": subCmd{
		description: "Display information about resctrl filesystem",
		f:           subCmdInfo,
	},
	"monitor": subCmd{
		description: "Monitor resctrl groups",
		f:           subCmdMonitor,
	},
}

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = usage

	// Define the main help flag manually
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

// nolint:errcheck
func usage() {
	f := flag.CommandLine.Output()
	fmt.Fprint(f, `Usage: rdt <command> [options]

Available commands:`)

	for _, c := range slices.Sorted(maps.Keys(subCmds)) {
		fmt.Fprintf(f, "\n  %-12s %s", c, subCmds[c].description)
	}

	fmt.Fprint(f, `

Use "rdt <command> --help" for more information about a command.
`)

	fmt.Fprint(f, "\nGlobal options:\n")
	flag.PrintDefaults()
}

func addGlobalFlags(flagset *flag.FlagSet) {
	flagset.StringVar(&groupPrefix, "group-prefix", "", "prefix to use for resctrl groups")
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

func subCmdMonitor(args []string) error {
	// Parse command line args
	flags := flag.NewFlagSet("configure", flag.ExitOnError)
	addGlobalFlags(flags)

	port := flags.Int("port", 8080, "port to serve metrics on")

	if err := flags.Parse(args); err != nil {
		return err
	}

	// Run sub-command
	if err := rdt.Initialize(groupPrefix); err != nil {
		return fmt.Errorf("RDT is not enabled: %v", err)
	}

	prometheusRegistry := prometheus.NewRegistry()
	prometheusRegistry.MustRegister(rdt.NewCollector())
	http.Handle("/metrics", promhttp.HandlerFor(prometheusRegistry, promhttp.HandlerOpts{}))

	fmt.Printf("Serving prometheus metrics at :%d/metrics\n", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
		return fmt.Errorf("error running HTTP server: %v", err)
	}
	return nil
}
