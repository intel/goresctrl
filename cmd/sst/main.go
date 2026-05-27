/*
Copyright 2026 Intel Corporation

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
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"sigs.k8s.io/yaml"

	"github.com/intel/goresctrl/pkg/log"
	goresctrlpath "github.com/intel/goresctrl/pkg/path"
	"github.com/intel/goresctrl/pkg/sst"
	"github.com/intel/goresctrl/pkg/utils"
)

var packageIds string

type subCmd struct {
	description string
	f           func([]string) error
}

var subCmds = map[string]subCmd{
	"status": {description: "Print current status of SST features", f: subCmdStatus},
	"info":   {description: "Print detailed SST-PP level info", f: subCmdInfo},
	"bf":     {description: "Configure SST-BF feature", f: subCmdBF},
	"cp":     {description: "Configure SST-CP feature", f: subCmdCP},
	"tf":     {description: "Configure SST-TF feature", f: subCmdTF},
}

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = usage

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
	fmt.Fprint(f, "Usage: sst [global-options] <command> [options]\n\nAvailable commands:")
	for _, c := range slices.Sorted(maps.Keys(subCmds)) {
		fmt.Fprintf(f, "\n  %-12s %s", c, subCmds[c].description)
	}
	fmt.Fprint(f, `

Use "sst <command> --help" for more information about a command.
`)
	fmt.Fprint(f, "\nGlobal options:\n")
	flag.PrintDefaults()
}

func addCommonPackageFlags(flagset *flag.FlagSet) {
	flagset.StringVar(&packageIds, "package", "", "Comma-separated physical package IDs (default: all)")
}

func addCommonFlags(flagset *flag.FlagSet) {
	flagset.Func("prefix", "set mount prefix for system directories", func(s string) error {
		goresctrlpath.SetPrefix(s)
		return nil
	})
}

func initHandle() (*sst.Platform, error) {
	return sst.Init()
}

func getPackageHandles(h *sst.Platform) ([]*sst.Package, error) {
	if packageIds == "" {
		return h.Packages(), nil
	}
	var handles []*sst.Package
	for _, idStr := range strings.Split(packageIds, ",") {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid package id %q: %w", idStr, err)
		}
		ph, ok := h.Package(id)
		if !ok {
			return nil, fmt.Errorf("package %d not found", id)
		}
		handles = append(handles, ph)
	}
	return handles, nil
}

func subCmdStatus(args []string) error {
	var format string
	flags := flag.NewFlagSet("status", flag.ExitOnError)
	flags.StringVar(&format, "format", "table", "Output format: table, yaml, json")
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}

	// Collect all package info.
	allInfo := make(map[utils.ID]*sst.PackageStatus, len(pkgs))
	for _, pkg := range pkgs {
		info, err := pkg.GetStatus()
		if err != nil {
			return fmt.Errorf("failed to get info for package %d: %w", pkg.ID(), err)
		}
		allInfo[utils.ID(pkg.ID())] = info
	}

	switch strings.ToLower(format) {
	case "json":
		data, err := json.MarshalIndent(allInfo, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal info: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(allInfo)
		if err != nil {
			return fmt.Errorf("failed to marshal info: %w", err)
		}
		fmt.Print(string(data))
	case "table":
		printStatusTable(allInfo)
	default:
		return fmt.Errorf("unknown format %q (valid: table, yaml, json)", format)
	}
	return nil
}

// nolint:errcheck
func printStatusTable(allInfo map[utils.ID]*sst.PackageStatus) {
	idSetStr := func(s utils.IDSet) string {
		if len(s) == 0 {
			return "none"
		}
		return s.String()
	}

	newTable := func() *tabwriter.Writer {
		return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	}

	pkgIDs := slices.Sorted(maps.Keys(allInfo))

	// PP table
	fmt.Println("SST-PP (Performance Profile):")
	w := newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tLOCKED\tVERSION\tCURRENT LEVEL\tMAX LEVEL\tLEVELS\tCPUS")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID].Punits)) {
			pu := allInfo[pkgID].Punits[punitID]
			fmt.Fprintf(w, "  %d\t%d\t%v\t%v\t%d\t%d\t%d\t%s\t%s\n",
				pkgID, punitID,
				pu.PP.Supported, pu.PP.Locked, pu.PP.Version,
				pu.PP.CurrentLevel, pu.PP.MaxLevel,
				idSetStr(utils.NewIDSetFromIntSlice(pu.PP.Levels...)),
				idSetStr(pu.CPUs))
		}
	}
	w.Flush()

	// BF table
	fmt.Println("\nSST-BF (Base Frequency):")
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tENABLED")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID].Punits)) {
			pu := allInfo[pkgID].Punits[punitID]
			fmt.Fprintf(w, "  %d\t%d\t%v\t%v\n",
				pkgID, punitID,
				pu.BF.Supported, pu.BF.Enabled)
		}
	}
	w.Flush()

	// TF table
	fmt.Println("\nSST-TF (Turbo Frequency):")
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tENABLED")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID].Punits)) {
			pu := allInfo[pkgID].Punits[punitID]
			fmt.Fprintf(w, "  %d\t%d\t%v\t%v\n",
				pkgID, punitID,
				pu.TF.Supported, pu.TF.Enabled)
		}
	}
	w.Flush()

	// CP table
	fmt.Println("\nSST-CP (Core Power):")
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tENABLED\tPRIORITY")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID].Punits)) {
			pu := allInfo[pkgID].Punits[punitID]
			prio := "-"
			if pu.CP.Supported {
				if pu.CP.Priority == sst.Ordered {
					prio = "ordered"
				} else {
					prio = "proportional"
				}
			}
			fmt.Fprintf(w, "  %d\t%d\t%v\t%v\t%s\n",
				pkgID, punitID,
				pu.CP.Supported, pu.CP.Enabled, prio)
		}
	}
	w.Flush()

	// CLOS tables
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID].Punits)) {
			pu := allInfo[pkgID].Punits[punitID]
			if !pu.CP.Supported || len(pu.Clos) == 0 {
				continue
			}
			fmt.Fprintf(os.Stdout, "\nSST-CP CLOS — Package %d Punit %d:\n", pkgID, punitID)
			cw := newTable()
			fmt.Fprintln(cw, "  CLOS\tMIN FREQ (MHz)\tMAX FREQ (MHz)\tPROPORTIONAL\tCPUS")
			for i, ci := range pu.Clos {
				fmt.Fprintf(cw, "  %d\t%d\t%d\t%d\t%s\n",
					i, ci.Config.MinFreq, ci.Config.MaxFreq,
					ci.Config.ProportionalPriority, idSetStr(ci.CPUs))
			}
			cw.Flush()
		}
	}
}

func subCmdInfo(args []string) error {
	level := -1
	var format string
	flags := flag.NewFlagSet("info", flag.ExitOnError)
	flags.IntVar(&level, "level", -1, "Performance level to query (default: current level of the first package/punit)")
	flags.StringVar(&format, "format", "table", "Output format: table, yaml, json")
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if level < -1 {
		return fmt.Errorf("invalid level %d: must be >= -1 (-1 uses the current level)", level)
	}

	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found")
	}

	// As default, use the perf level of the first package's first punit.
	if level < 0 {
		status, err := pkgs[0].GetStatus()
		if err != nil {
			return fmt.Errorf("failed to get status for package %d: %w", pkgs[0].ID(), err)
		}
		punits := slices.Sorted(maps.Keys(status.Punits))
		if len(punits) == 0 {
			return fmt.Errorf("package %d has no punits, cannot determine default level", pkgs[0].ID())
		}
		level = status.Punits[punits[0]].PP.CurrentLevel
	}

	type infoOutput struct {
		Level    int                                          `json:"level"`
		Packages map[utils.ID]map[utils.ID]*sst.PerfLevelInfo `json:"packages"`
	}
	out := infoOutput{
		Level:    level,
		Packages: make(map[utils.ID]map[utils.ID]*sst.PerfLevelInfo, len(pkgs)),
	}
	for _, pkg := range pkgs {
		info, err := pkg.GetPerfLevelInfo(level)
		if err != nil {
			return fmt.Errorf("failed to get level %d info for package %d: %w", level, pkg.ID(), err)
		}
		out.Packages[utils.ID(pkg.ID())] = info
	}

	switch strings.ToLower(format) {
	case "json":
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal info: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(out)
		if err != nil {
			return fmt.Errorf("failed to marshal info: %w", err)
		}
		fmt.Print(string(data))
	case "table":
		printInfoTable(level, out.Packages)
	default:
		return fmt.Errorf("unknown format %q (valid: table, yaml, json)", format)
	}
	return nil
}

// nolint:errcheck
func printInfoTable(level int, allInfo map[utils.ID]map[utils.ID]*sst.PerfLevelInfo) {
	freqsStr := func(freqs []sst.TRLFreqInfo) string {
		if len(freqs) == 0 {
			return "none"
		}
		parts := make([]string, len(freqs))
		for i, f := range freqs {
			parts[i] = strconv.Itoa(f.ID) + ":" + strconv.Itoa(f.Freq)
		}
		return strings.Join(parts, ", ")
	}

	idSetStr := func(s utils.IDSet) string {
		if len(s) == 0 {
			return "none"
		}
		return s.String()
	}

	newTable := func() *tabwriter.Writer {
		return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	}

	pkgIDs := slices.Sorted(maps.Keys(allInfo))

	// PP table
	fmt.Printf("SST-PP (Perf Profile) [Level %d]:\n", level)
	w := newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tCPUS")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID])) {
			pu := allInfo[pkgID][punitID]
			fmt.Fprintf(w, "  %d\t%d\t%s\n", pkgID, punitID, idSetStr(pu.CPUs))
		}
	}
	w.Flush()

	// BF table
	fmt.Printf("\nSST-BF (Base Frequency) [Level %d]:\n", level)
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tHP BASE FREQ (MHz)\tLP BASE FREQ (MHz)\tHP CPUS")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID])) {
			bf := allInfo[pkgID][punitID].BF
			hpFreq, lpFreq, hpCPUs := "-", "-", "-"
			if bf.Supported {
				hpFreq = strconv.Itoa(bf.HighPriorityBaseFreq)
				lpFreq = strconv.Itoa(bf.LowPriorityBaseFreq)
				hpCPUs = idSetStr(bf.HighPriorityCPUs)
			}
			fmt.Fprintf(w, "  %d\t%d\t%v\t%s\t%s\t%s\n",
				pkgID, punitID, bf.Supported, hpFreq, lpFreq, hpCPUs)
		}
	}
	w.Flush()

	// TF tables
	fmt.Printf("\nSST-TF (Turbo Frequency) [Level %d]:\n", level)
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tSUPPORTED\tLP CLIP FREQS PER TRL ID (MHz)")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID])) {
			tf := allInfo[pkgID][punitID].TF
			lpClip := "-"
			if tf.Supported {
				lpClip = freqsStr(tf.LPClipFreqs)
			}
			fmt.Fprintf(w, "  %d\t%d\t%v\t%s\n", pkgID, punitID, tf.Supported, lpClip)
		}
	}
	w.Flush()

	fmt.Printf("\nSST-TF (Turbo Frequency) Buckets [Level %d]:\n", level)
	w = newTable()
	fmt.Fprintln(w, "  PKG\tPUNIT\tBUCKET\tHP CORES\tMAX FREQS PER TRL ID (MHz)")
	for _, pkgID := range pkgIDs {
		for _, punitID := range slices.Sorted(maps.Keys(allInfo[pkgID])) {
			for _, bucket := range allInfo[pkgID][punitID].TF.Buckets {
				fmt.Fprintf(w, "  %d\t%d\t%d\t%d\t%s\n",
					pkgID, punitID, bucket.ID, bucket.HighPriorityCoreCount, freqsStr(bucket.MaxFreqs))
			}
		}
	}
	w.Flush()
}

func subCmdBF(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: bf <enable|disable> [options]")
	}
	sub := args[0]
	flags := flag.NewFlagSet("bf "+sub, flag.ExitOnError)
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	switch sub {
	case "enable":
		for _, pkg := range pkgs {
			fmt.Printf("Enabling BF for package %d\n", pkg.ID())
			if err := pkg.BFEnable(); err != nil {
				return fmt.Errorf("failed to enable BF for package %d: %w", pkg.ID(), err)
			}
		}
	case "disable":
		for _, pkg := range pkgs {
			fmt.Printf("Disabling BF for package %d\n", pkg.ID())
			if err := pkg.BFDisable(); err != nil {
				return fmt.Errorf("failed to disable BF for package %d: %w", pkg.ID(), err)
			}
		}
	default:
		return fmt.Errorf("unknown bf sub-command %q (valid: enable, disable)", sub)
	}
	return nil
}

func subCmdTF(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: tf <enable|disable> [options]")
	}
	sub := args[0]
	flags := flag.NewFlagSet("tf "+sub, flag.ExitOnError)
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	switch sub {
	case "enable":
		for _, pkg := range pkgs {
			fmt.Printf("Enabling TF for package %d\n", pkg.ID())
			if err := pkg.TFEnable(); err != nil {
				return fmt.Errorf("failed to enable TF for package %d: %w", pkg.ID(), err)
			}
		}
	case "disable":
		for _, pkg := range pkgs {
			fmt.Printf("Disabling TF for package %d\n", pkg.ID())
			if err := pkg.TFDisable(); err != nil {
				return fmt.Errorf("failed to disable TF for package %d: %w", pkg.ID(), err)
			}
		}
	default:
		return fmt.Errorf("unknown tf sub-command %q (valid: enable, disable)", sub)
	}
	return nil
}

func subCmdCP(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: cp <enable|disable|reset|configure|set-priority|associate> [options]")
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "enable":
		return subCmdCPEnable(rest)
	case "disable":
		return subCmdCPDisable(rest)
	case "reset":
		return subCmdCPReset(rest)
	case "configure":
		return subCmdCPConfigure(rest)
	case "set-priority":
		return subCmdCPSetPriority(rest)
	case "associate":
		return subCmdCPAssociate(rest)
	default:
		return fmt.Errorf("unknown cp sub-command %q (valid: enable, disable, reset, configure, set-priority, associate)", sub)
	}
}

func subCmdCPEnable(args []string) error {
	flags := flag.NewFlagSet("cp enable", flag.ExitOnError)
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		fmt.Printf("Enabling CP for package %d\n", pkg.ID())
		if err := pkg.CPEnable(); err != nil {
			return fmt.Errorf("failed to enable CP for package %d: %w", pkg.ID(), err)
		}
	}
	return nil
}

func subCmdCPDisable(args []string) error {
	flags := flag.NewFlagSet("cp disable", flag.ExitOnError)
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		fmt.Printf("Disabling CP for package %d\n", pkg.ID())
		if err := pkg.CPDisable(); err != nil {
			return fmt.Errorf("failed to disable CP for package %d: %w", pkg.ID(), err)
		}
	}
	return nil
}

func subCmdCPReset(args []string) error {
	flags := flag.NewFlagSet("cp reset", flag.ExitOnError)
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		fmt.Printf("Resetting CP config for package %d\n", pkg.ID())
		if err := pkg.CPReset(); err != nil {
			return fmt.Errorf("failed to reset CP config for package %d: %w", pkg.ID(), err)
		}
	}
	return nil
}

func subCmdCPConfigure(args []string) error {
	var closID int
	var minFreq, maxFreq, proportional int
	flags := flag.NewFlagSet("cp configure", flag.ExitOnError)
	flags.IntVar(&closID, "clos", 0, "CLOS id (0-3)")
	flags.IntVar(&minFreq, "min", 0, "Minimum frequency in MHz")
	flags.IntVar(&maxFreq, "max", 25500, "Maximum frequency in MHz")
	flags.IntVar(&proportional, "proportional", 0, "Proportional priority (0-15)")
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	closConfig := sst.ClosConfig{
		MinFreq:              minFreq,
		MaxFreq:              maxFreq,
		ProportionalPriority: proportional,
	}
	for _, pkg := range pkgs {
		fmt.Printf("Configuring CLOS %d for package %d\n", closID, pkg.ID())
		if err := pkg.ClosConfigure(closID, closConfig); err != nil {
			return fmt.Errorf("failed to configure CLOS %d for package %d: %w", closID, pkg.ID(), err)
		}
	}
	return nil
}

func subCmdCPSetPriority(args []string) error {
	var priorityStr string
	flags := flag.NewFlagSet("cp set-priority", flag.ExitOnError)
	flags.StringVar(&priorityStr, "priority", "", "Priority type: ordered or proportional")
	addCommonFlags(flags)
	addCommonPackageFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	var priority sst.CPPriorityType
	switch strings.ToLower(priorityStr) {
	case "ordered":
		priority = sst.Ordered
	case "proportional":
		priority = sst.Proportional
	default:
		return fmt.Errorf("invalid priority %q (valid: ordered, proportional)", priorityStr)
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	pkgs, err := getPackageHandles(h)
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		fmt.Printf("Setting CP priority to %s for package %d\n", priorityStr, pkg.ID())
		if err := pkg.CPSetPriorityType(priority); err != nil {
			return fmt.Errorf("failed to set CP priority for package %d: %w", pkg.ID(), err)
		}
	}
	return nil
}

func subCmdCPAssociate(args []string) error {
	var closID int
	var cpusStr string
	flags := flag.NewFlagSet("cp associate", flag.ExitOnError)
	flags.IntVar(&closID, "clos", 0, "CLOS id (0-3)")
	flags.StringVar(&cpusStr, "cpus", "", "CPU IDs to associate (e.g. 0,1,2 or 0-3)")
	addCommonFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if cpusStr == "" {
		return fmt.Errorf("no CPUs specified (-cpus required)")
	}
	cpus, err := utils.NewIDSetFromString(cpusStr)
	if err != nil {
		return fmt.Errorf("invalid CPU list %q: %w", cpusStr, err)
	}
	h, err := initHandle()
	if err != nil {
		return err
	}
	fmt.Printf("Associating CPUs %s to CLOS %d\n", cpus.String(), closID)
	return h.ClosAssociate(closID, cpus)
}
