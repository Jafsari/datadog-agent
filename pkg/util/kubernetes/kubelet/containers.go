// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build kubelet,linux

package kubelet

import (
	"fmt"
	"strings"

	"github.com/DataDog/datadog-agent/pkg/util/containers"
	"github.com/DataDog/datadog-agent/pkg/util/containers/metrics"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

func (ku *KubeUtil) Containers() ([]*containers.Container, error) {
	pods, err := ku.GetLocalPodList()
	if err != nil {
		return nil, err
	}

	cgByContainer, err := metrics.ScrapeAllCgroups()
	if err != nil {
		return nil, fmt.Errorf("could not get cgroups: %s", err)
	}

	var ctrList []*containers.Container

	for _, pod := range pods {
		for _, c := range pod.Status.Containers {
			container, err := parseContainerInPod(c, pod)
			if err != nil {
				log.Debugf("Cannot parse container %s in pod %s: %s", c.ID, pod.Metadata.Name, err)
				continue
			}
			if container == nil {
				// Skip nil containers
				continue
			}
			cgroup, ok := cgByContainer[container.ID]
			if !ok {
				log.Debugf("No cgroup found for container %s in pod %s, skipping", container.ID, pod.Metadata.Name)
				continue
			}
			container.SetCgroups(cgroup)
			ctrList = append(ctrList, container)

			err = container.FillCgroupLimits()
			if err != nil {
				log.Debugf("Cannot get limits for container %s: %s, skipping", container.ID, err)
				continue
			}
		}
	}

	log.Debugf("Got %d containers", len(ctrList))

	for _, container := range ctrList {
		err = container.FillCgroupMetrics()
		if err != nil {
			log.Debugf("Cannot get metrics for container %s: %s", container.ID, err)
			continue
		}
		err = container.FillNetworkMetrics(nil)
		if err != nil {
			log.Debugf("Cannot get network stats for container %s: %s", container.ID, err)
			continue
		}
	}

	return ctrList, nil
}

func parseContainerInPod(status ContainerStatus, pod *Pod) (*containers.Container, error) {
	c := &containers.Container{
		Type:     "kubelet",
		ID:       TrimRuntimeFromCID(status.ID),
		EntityID: status.ID,
		Name:     status.Name,
		Image:    status.Image,
		// Fake
		Health:   "",
		Excluded: false,
	}

	switch {
	case status.State.Running != nil:
		c.State = containers.ContainerRunningState
		c.Created = status.State.Running.StartedAt.Unix()
	case status.State.Waiting != nil:
		// TODO We don't handle waiting container yet
		return nil, nil
	case status.State.Terminated != nil:
		// TODO look at reason
		c.State = containers.ContainerExitedState
		c.Created = status.State.Terminated.StartedAt.Unix()
	default:
		return nil, fmt.Errorf("container %s is in an unknown state", c.ID)
	}

	return c, nil
}

func TrimRuntimeFromCID(cid string) string {
	parts := strings.SplitN(cid, "://", 2)
	return parts[len(parts)-1]
}
