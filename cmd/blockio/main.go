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
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/intel/goresctrl/pkg/blockio"
	goresctrlpath "github.com/intel/goresctrl/pkg/path"
)

var examples string = `Examples:
    # Inspect OCI blockio structure
    $ blockio -config sample.cfg -class slowread | jq

    # Apply read throttling to a cgroup
    $ blockio -config sample.cfg -class slowread -cgroup user.slice/mygroup
    $ cat /sys/fs/cgroup/blkio/user.slice/mygroup/blkio.throttle.read_bps_device

    # Remove throttling from a cgroup
    $ blockio -config sample.cfg -class nolimit -cgroup user.slice/mygroup
`

func usage() {
	flag.CommandLine.SetOutput(os.Stdout)
	fmt.Fprintln(flag.CommandLine.Output(), "blockio - demo application for goresctrl/pkg/blockio API")
	fmt.Fprintln(flag.CommandLine.Output(), "Usage: blockio -config=FILE -class=NAME [-cgroup=CGROUP]")
	flag.PrintDefaults()
	fmt.Fprint(flag.CommandLine.Output(), examples)
}

func errorExit(format string, args ...interface{}) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, args...))
	os.Exit(1)
}

func main() {
	// Parse commandline arguments
	flag.Usage = usage
	flag.Func("prefix", "set mount prefix for system directories", func(s string) error {
		goresctrlpath.SetPrefix(s)
		return nil
	})
	optConfig := flag.String("config", "", "load class configuration from FILE")
	optClass := flag.String("class", "", "use configuration of the blockio class NAME")
	optCgroup := flag.String("cgroup", "", "apply class to CGROUP, otherwise print it as OCI BlockIO structure")
	flag.Parse()

	if optConfig == nil || *optConfig == "" {
		errorExit("missing -config=FILE")
	}

	if optClass == nil || *optClass == "" {
		errorExit("missing -class=NAME")
	}

	// Read blockio class configuration.
	if err := blockio.SetConfigFromFile(*optConfig, true); err != nil {
		errorExit("%v", err)
	}

	if optCgroup == nil || *optCgroup == "" {
		// If -cgroup=CGROUP is missing, print OCI spec.
		oci, err := blockio.OciLinuxBlockIO(*optClass)
		if err != nil {
			errorExit("%v", err)
		}
		ociBytes, err := json.Marshal(oci)
		if err != nil {
			errorExit("%v", err)
		}
		fmt.Printf("%s\n", ociBytes)
	} else {
		// If -cgroup=CGROUP is given, apply class configuration to it.
		err := blockio.SetCgroupClass(*optCgroup, *optClass)
		if err != nil {
			errorExit("%v", err)
		}
		fmt.Printf("cgroup %s configured to blockio class %q\n", *optCgroup, *optClass)
	}
}
