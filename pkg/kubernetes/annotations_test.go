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

package kubernetes

import (
	"testing"
)

// TestContainerClassFromAnnotations: unit test for ContainerClassFromAnnotations.
func TestContainerClassFromAnnotations(t *testing.T) {
	aContainerAnnotation := "io.kubernetes.cri.a-class"
	aPodAnnotation := "a.resources.beta.kubernetes.io/pod"
	aPodAnnotationContainerPrefix := "a.resources.beta.kubernetes.io/container."
	bContainerAnnotation := "io.kubernetes.cri.b-class"
	bPodAnnotation := "b.resources.beta.kubernetes.io/pod"
	bPodAnnotationContainerPrefix := "b.resources.beta.kubernetes.io/container."
	allContainerAnnotations := map[string]string{
		aContainerAnnotation: "a-container-class",
		bContainerAnnotation: "b-container-class",
	}
	allPodAnnotations := map[string]string{
		aPodAnnotation: "a-pod-class",
		aPodAnnotationContainerPrefix + "special-container": "a-pod-container-class",
		bPodAnnotation: "b-pod-class",
		bPodAnnotationContainerPrefix + "special-container": "b-pod-container-class",
	}
	tcases := []struct {
		name string // the name of the test case
		// inputs
		lookForCA  string            // container annotation to look for
		lookForPA  string            // pod annotation to look for
		lookForPAC string            // pod annotation container prefix to look for
		cName      string            // container name
		cAnns      map[string]string // container annotations
		pAnns      map[string]string // pod annotations
		// outputs
		expectedClass  string
		expectedOrigin ClassOrigin
	}{
		{
			name:           "all empty",
			expectedOrigin: ClassOriginNotFound,
		},
		{
			name:           "container annotation overrides all pod annotations",
			lookForCA:      aContainerAnnotation,
			lookForPA:      aPodAnnotation,
			lookForPAC:     aPodAnnotationContainerPrefix,
			cName:          "special-container",
			cAnns:          allContainerAnnotations,
			pAnns:          allPodAnnotations,
			expectedClass:  "a-container-class",
			expectedOrigin: ClassOriginContainerAnnotation,
		},
		{
			name:           "container prefix overrides default pod annotation",
			lookForCA:      "not.existing.container.annotation",
			lookForPA:      bPodAnnotation,
			lookForPAC:     bPodAnnotationContainerPrefix,
			cName:          "special-container",
			cAnns:          allContainerAnnotations,
			pAnns:          allPodAnnotations,
			expectedClass:  "b-pod-container-class",
			expectedOrigin: ClassOriginPodAnnotation,
		},
		{
			name:           "default pod annotation",
			lookForCA:      "not.existing.container.annotation",
			lookForPA:      bPodAnnotation,
			lookForPAC:     bPodAnnotationContainerPrefix,
			cName:          "ordinary-container",
			cAnns:          allContainerAnnotations,
			pAnns:          allPodAnnotations,
			expectedClass:  "b-pod-class",
			expectedOrigin: ClassOriginPodAnnotation,
		},
	}
	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			observedClass, observedOrigin := ContainerClassFromAnnotations(
				tc.lookForCA, tc.lookForPA, tc.lookForPAC,
				tc.cName, tc.cAnns, tc.pAnns)
			if observedClass != tc.expectedClass {
				t.Errorf("expected class %q, observed %q", tc.expectedClass, observedClass)
			}
			if observedOrigin != tc.expectedOrigin {
				t.Errorf("expected origin %q, observed %q", tc.expectedOrigin, observedOrigin)
			}
		})
	}

}
