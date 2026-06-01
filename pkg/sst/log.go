/*
Copyright 2021-2026 Intel Corporation

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

package sst

import (
	"log/slog"

	"github.com/intel/goresctrl/pkg/sst/internal/isst"
)

var sstlog *slog.Logger = slog.Default()

// SetLogger sets the logger instance used by the package and its sub-packages.
func SetLogger(l *slog.Logger) {
	sstlog = l
	isst.SetLogger(l)
}
