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

const (
	// BlockioContainerAnnotation is the CRI level container annotation for setting
	// the blockio class of a container
	BlockioContainerAnnotation = "io.kubernetes.cri.blockio-class"

	// BlockioPodAnnotation is a Pod annotation for setting the blockio class of
	// all containers of the pod
	BlockioPodAnnotation = "blockio.resources.beta.kubernetes.io/pod"

	// BlockioPodAnnotationContainerPrefix is prefix for per-container Pod annotation
	// for setting the blockio class of one container of the pod
	BlockioPodAnnotationContainerPrefix = "blockio.resources.beta.kubernetes.io/container."

	// RdtContainerAnnotation is the CRI level container annotation for setting
	// the RDT class (CLOS) of a container
	RdtContainerAnnotation = "io.kubernetes.cri.rdt-class"

	// RdtPodAnnotation is a Pod annotation for setting the RDT class (CLOS) of
	// all containers of the pod
	RdtPodAnnotation = "rdt.resources.beta.kubernetes.io/pod"

	// RdtPodAnnotationContainerPrefix is prefix for per-container Pod annotation
	// for setting the RDT class (CLOS) of one container of the pod
	RdtPodAnnotationContainerPrefix = "rdt.resources.beta.kubernetes.io/container."
)

type ClassOrigin int

const (
	ClassOriginNotFound ClassOrigin = iota
	ClassOriginContainerAnnotation
	ClassOriginPodAnnotation
)

func (c ClassOrigin) String() string {
	switch c {
	case ClassOriginNotFound:
		return "<not found>"
	case ClassOriginContainerAnnotation:
		return "container annotations"
	case ClassOriginPodAnnotation:
		return "pod annotations"
	default:
		return "<unknown>"
	}
}

// ContainerClassFromAnnotations determines the effective class of a
// container from the Pod annotations and CRI level container
// annotations of a container.
func ContainerClassFromAnnotations(containerAnnotation, podAnnotation, podAnnotationContainerPrefix string, containerName string, containerAnnotations, podAnnotations map[string]string) (string, ClassOrigin) {
	if clsName, ok := containerAnnotations[containerAnnotation]; ok {
		return clsName, ClassOriginContainerAnnotation
	}
	if clsName, ok := podAnnotations[podAnnotationContainerPrefix+containerName]; ok {
		return clsName, ClassOriginPodAnnotation
	}
	if clsName, ok := podAnnotations[podAnnotation]; ok {
		return clsName, ClassOriginPodAnnotation
	}
	return "", ClassOriginNotFound
}
