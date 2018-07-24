// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build kubelet

package kubelet

import "time"

// Pod contains fields for unmarshalling a Pod
type Pod struct {
	Spec     Spec        `json:"spec,omitempty"`
	Status   Status      `json:"status,omitempty"`
	Metadata PodMetadata `json:"metadata,omitempty"`
}

// PodList contains fields for unmarshalling a PodList
type PodList struct {
	Items []*Pod `json:"items,omitempty"`
}

// PodMetadata contains fields for unmarshalling a pod's metadata
type PodMetadata struct {
	Name        string            `json:"name,omitempty"`
	UID         string            `json:"uid,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	ResVersion  string            `json:"resourceVersion,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Owners      []PodOwner        `json:"ownerReferences,omitempty"`
}

// PodOwner contains fields for unmarshalling a Pod.Metadata.Owners
type PodOwner struct {
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	ID   string `json:"uid,omitempty"`
}

// Spec contains fields for unmarshalling a Pod.Spec
type Spec struct {
	HostNetwork bool            `json:"hostNetwork,omitempty"`
	NodeName    string          `json:"nodeName,omitempty"`
	Containers  []ContainerSpec `json:"containers,omitempty"`
}

// ContainerSpec contains fields for unmarshalling a Pod.Spec.Containers
type ContainerSpec struct {
	Name  string              `json:"name"`
	Image string              `json:"image,omitempty"`
	Ports []ContainerPortSpec `json:"ports,omitempty"`
}

// ContainerSpec contains fields for unmarshalling a Pod.Spec.Containers.Ports
type ContainerPortSpec struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort"`
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
}

// Status contains fields for unmarshalling a Pod.Status
type Status struct {
	Phase      string            `json:"phase,omitempty"`
	HostIP     string            `json:"hostIP,omitempty"`
	PodIP      string            `json:"podIP,omitempty"`
	Containers []ContainerStatus `json:"containerStatuses,omitempty"`
	Conditions []Conditions      `json:"conditions,omitempty"`
}

// Conditions contains fields for unmarshalling a Pod.Status.Conditions
type Conditions struct {
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}

// ContainerStatus contains fields for unmarshalling a Pod.Status.Containers
type ContainerStatus struct {
	Name  string         `json:"name,omitempty"`
	Image string         `json:"image,omitempty"`
	ID    string         `json:"containerID,omitempty"`
	State ContainerState `json:"state"`
}

// ContainerState holds a possible state of container.
// Only one of its members may be specified.
// If none of them is specified, the default one is ContainerStateWaiting.
type ContainerState struct {
	// Details about a waiting container
	// +optional
	Waiting *ContainerStateWaiting `json:"waiting,omitempty" protobuf:"bytes,1,opt,name=waiting"`
	// Details about a running container
	// +optional
	Running *ContainerStateRunning `json:"running,omitempty" protobuf:"bytes,2,opt,name=running"`
	// Details about a terminated container
	// +optional
	Terminated *ContainerStateTerminated `json:"terminated,omitempty" protobuf:"bytes,3,opt,name=terminated"`
}

// ContainerStateWaiting is a waiting state of a container.
type ContainerStateWaiting struct {
	// (brief) reason the container is not yet running.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,1,opt,name=reason"`
	// Message regarding why the container is not yet running.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,2,opt,name=message"`
}

// ContainerStateRunning is a running state of a container.
type ContainerStateRunning struct {
	// Time at which the container was last (re-)started
	// +optional
	StartedAt time.Time `json:"startedAt,omitempty" protobuf:"bytes,1,opt,name=startedAt"`
}

// ContainerStateTerminated is a terminated state of a container.
type ContainerStateTerminated struct {
	// Exit status from the last termination of the container
	ExitCode int32 `json:"exitCode" protobuf:"varint,1,opt,name=exitCode"`
	// Signal from the last termination of the container
	// +optional
	Signal int32 `json:"signal,omitempty" protobuf:"varint,2,opt,name=signal"`
	// (brief) reason from the last termination of the container
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,3,opt,name=reason"`
	// Message regarding the last termination of the container
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`
	// Time at which previous execution of the container started
	// +optional
	StartedAt time.Time `json:"startedAt,omitempty" protobuf:"bytes,5,opt,name=startedAt"`
	// Time at which the container last terminated
	// +optional
	FinishedAt time.Time `json:"finishedAt,omitempty" protobuf:"bytes,6,opt,name=finishedAt"`
	// Container's ID in the format 'docker://<container_id>'
	// +optional
	ContainerID string `json:"containerID,omitempty" protobuf:"bytes,7,opt,name=containerID"`
}
