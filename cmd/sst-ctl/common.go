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
	"fmt"
	"strconv"
	"strings"

	"github.com/intel/goresctrl/pkg/sst"
	"github.com/intel/goresctrl/pkg/utils"
)

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

		id, err := strconv.ParseInt(str, 10, 0)
		if err != nil {
			fmt.Printf("invalid value '%s': %v", str, err)
			continue
		}

		s = append(s, int(id))
	}

	return s
}
