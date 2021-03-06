// Copyright 2021 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This module provides the native implementation of the filesystem
// interface (fsi).

package cgroups

import (
	"os"
	"path/filepath"
)

type fsOs struct{}

type osFile struct {
	file *os.File
}

func newFsiOS() fsiIface {
	return fsOs{}
}

func (fsOs) OpenFile(name string, flag int, perm os.FileMode) (fileIface, error) {
	f, err := os.OpenFile(name, flag, perm)
	return osFile{f}, err
}

func (fsOs) Open(name string) (fileIface, error) {
	f, err := os.Open(name)
	return osFile{f}, err
}

func (fsOs) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

func (fsOs) Walk(root string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(root, walkFn)
}

func (osf osFile) Write(b []byte) (n int, err error) {
	return osf.file.Write(b)
}

func (osf osFile) Read(b []byte) (n int, err error) {
	return osf.file.Read(b)
}

func (osf osFile) Close() error {
	return osf.file.Close()
}
