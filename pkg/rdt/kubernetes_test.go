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

package rdt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerClassFromAnnotations(t *testing.T) {
	containerName := "test-container"
	containerAnnotations := map[string]string{}
	podAnnotations := map[string]string{}

	// Helper function for checking test cases
	tc := func(expectError bool, expectedClsName string) {
		cls, err := ContainerClassFromAnnotations(containerName, containerAnnotations, podAnnotations)
		if !expectError && err != nil {
			t.Errorf("unexpected error: %v", err)
		} else if expectError && err == nil {
			t.Errorf("unexpected success setting RDT class to %q", cls)
		} else if cls != expectedClsName {
			t.Errorf("invalid rdt class, expecting %q, got %q", expectedClsName, cls)
		}
	}

	//
	// 1. Test container annotation
	//

	// Should succeed when rdt is uninitialized but annotations are empty
	rdt = nil
	tc(false, "")

	// Should fail when rdt is uninitialized but annotations point to a class
	containerAnnotations = map[string]string{RdtContainerAnnotation: "class-1"}
	podAnnotations = map[string]string{
		RdtPodAnnotationContainerPrefix + containerName: "class-2",
		RdtPodAnnotation: "class-3"}
	tc(true, "")

	// Mock configured rdt which enables the functionality
	mockFs, err := newMockResctrlFs(t, "resctrl.l2", "")
	require.NoError(t, err, "failed to set up mock resctrl fs")
	defer mockFs.delete()

	require.NoError(t, Initialize(""), "rdt initialization failed")

	// Should fail with an empty set of classes
	tc(true, "")

	// Should succeed when the class exists but no configuration has been set ("discovery mode")
	require.NoError(t, mockFs.createCtrlGroup("class-1"))
	require.NoError(t, mockFs.createCtrlGroup("class-2"))
	require.NoError(t, mockFs.createCtrlGroup("class-3"))
	tc(false, "class-1")

	// Should succeed with default class config
	rdt.conf.Classes = classSet{"class-1": &classConfig{}, "class-2": &classConfig{}, "class-3": &classConfig{}}
	tc(false, "class-1")

	// Should fail when container annotation has been denied in class ocnfig
	rdt.conf.Classes["class-1"].Kubernetes.DenyContainerAnnotation = true
	tc(true, "")

	// Test invalid class name
	containerAnnotations[RdtContainerAnnotation] = "foo/bar"
	tc(true, "")

	//
	// 2. Test per-container Pod annotation
	//
	delete(containerAnnotations, RdtContainerAnnotation)
	tc(false, "class-2")

	// Should fail when pod annotations for the class are denied
	rdt.conf.Classes["class-2"].Kubernetes.DenyPodAnnotation = true
	tc(true, "")

	//
	// 3. Test pod-wide Pod annotation
	//
	delete(podAnnotations, RdtPodAnnotationContainerPrefix+containerName)
	tc(false, "class-3")

	// Should fail when pod annotations for the class are denied
	rdt.conf.Classes["class-3"].Kubernetes.DenyPodAnnotation = true
	tc(true, "")

	//
	// Test empty annotations
	//
	delete(podAnnotations, RdtPodAnnotation)
	tc(false, "")
}
