// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build kubeapiserver

package apiserver

import (
	"fmt"
	"time"

	utilcache "github.com/DataDog/datadog-agent/pkg/util/cache"
	"github.com/DataDog/datadog-agent/pkg/util/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	maxRetries = 15
	numWorkers = 2
)

// MetadataController is responsible for synchronizing Endpoints objects from the Kubernetes
// apiserver to build and cache the MetadataMapperBundle for each node.
// This controller only supports Kubernetes 1.4+.
type MetadataController struct {
	nodeLister       corelisters.NodeLister
	nodeListerSynced cache.InformerSynced

	endpointsLister       corelisters.EndpointsLister
	endpointsListerSynced cache.InformerSynced

	// Endpoints that need to be added to services mapping.
	queue workqueue.RateLimitingInterface
}

func NewMetadataController(nodeInformer coreinformers.NodeInformer, endpointsInformer coreinformers.EndpointsInformer) *MetadataController {
	m := &MetadataController{
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "metadata"),
	}
	m.nodeLister = nodeInformer.Lister()
	m.nodeListerSynced = nodeInformer.Informer().HasSynced

	endpointsInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    m.addEndpoints,
			UpdateFunc: m.updateEndpoints,
			DeleteFunc: m.deleteEndpoints,
		},
		metadataMapExpire/2, // delay for re-listing endpoints
	)
	m.endpointsLister = endpointsInformer.Lister()
	m.endpointsListerSynced = endpointsInformer.Informer().HasSynced

	return m
}

func (m *MetadataController) Run(stopCh <-chan struct{}) {
	defer m.queue.ShutDown()

	log.Infof("Starting metadata controller")
	defer log.Infof("Stopping metadata controller")

	if !cache.WaitForCacheSync(stopCh, m.nodeListerSynced, m.endpointsListerSynced) {
		return
	}

	for i := 0; i < numWorkers; i++ {
		go wait.Until(m.worker, time.Second, stopCh)
	}

	<-stopCh
}

func (m *MetadataController) worker() {
	for m.processNextWorkItem() {
	}
}

func (m *MetadataController) processNextWorkItem() bool {
	key, quit := m.queue.Get()
	if quit {
		return false
	}
	defer m.queue.Done(key)

	err := m.reconcileKey(key.(string))
	if err == nil {
		m.queue.Forget(key)
		return true
	}

	if m.queue.NumRequeues(key) < maxRetries {
		log.Debugf("Error syncing endpoints %v: %v", key, err)
		m.queue.AddRateLimited(key)
		return true
	}

	log.Debugf("Dropping endpoints %q out of the queue: %v", key, err)
	m.queue.Forget(key)

	return true
}

func (m *MetadataController) addEndpoints(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	log.Tracef("Adding endpoints %s", endpoints.Name)
	m.enqueue(obj)
}

func (m *MetadataController) updateEndpoints(_, new interface{}) {
	newEndpoints := new.(*corev1.Endpoints)
	log.Tracef("Updating endpoints %s", newEndpoints.Name)
	m.enqueue(new)
}

func (m *MetadataController) deleteEndpoints(obj interface{}) {
	endpoints, ok := obj.(*corev1.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Debugf("Couldn't get object from tombstone %#v", obj)
			return
		}
		endpoints, ok = tombstone.Obj.(*corev1.Endpoints)
		if !ok {
			log.Debugf("Tombstone contained object that is not an endpoint %#v", obj)
			return
		}
	}
	log.Tracef("Deleting endpoints %s", endpoints.Name)
	m.enqueue(obj)
}

func (m *MetadataController) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		log.Debugf("Couldn't get key for object %v: %v", obj, err)
		return
	}
	m.queue.Add(key)
}

func (m *MetadataController) reconcileKey(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	endpoints, err := m.endpointsLister.Endpoints(namespace).Get(name)
	switch {
	case errors.IsNotFound(err):
		// Endpoints absence in store means watcher caught the deletion, ensure metadata map is cleaned
		log.Tracef("Endpoints has been deleted %v. Attempting to cleanup metadata map", key)
		err = m.mapDeletedService(namespace, name)
	case err != nil:
		log.Debugf("Unable to retrieve endpoints %v from store: %v", key, err)
	default:
		err = m.mapService(endpoints)
	}
	return err
}

func (m *MetadataController) mapService(endpoints *corev1.Endpoints) error {
	nodeToPods := make(map[string]map[string]string)

	// Loop over the subsets to create a mapping of nodes to pods running on the node.
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef == nil {
				continue
			}
			if address.TargetRef.Kind != "Pod" {
				continue
			}
			namespace := address.TargetRef.Namespace
			podName := address.TargetRef.Name
			if podName == "" || namespace == "" {
				continue
			}

			// TODO: Kubernetes 1.3.x does not include `NodeName`
			if address.NodeName == nil {
				continue
			}

			nodeName := *address.NodeName

			if _, ok := nodeToPods[nodeName]; !ok {
				nodeToPods[nodeName] = make(map[string]string)
			}
			nodeToPods[nodeName][namespace] = podName
		}
	}

	svc := endpoints.Name
	for nodeName, pods := range nodeToPods {
		metaBundle, err := getMetadataMapBundle(nodeName)
		if err != nil {
			log.Tracef("Could not get metadata for node %s", nodeName)
			metaBundle = newMetadataMapperBundle()
		}

		metaBundle.m.Lock()
		for namespace, podName := range pods {
			metaBundle.Services.Set(namespace, podName, svc)
		}
		metaBundle.m.Unlock()

		cacheKey := utilcache.BuildAgentKey(metadataMapperCachePrefix, nodeName)

		utilcache.Cache.Set(cacheKey, metaBundle, metadataMapExpire)
	}

	return nil
}

func (m *MetadataController) mapDeletedService(namespace, svc string) error {
	nodes, err := m.nodeLister.List(labels.NewSelector()) // list all nodes
	if err != nil {
		return err
	}
	for _, node := range nodes {
		metaBundle, err := getMetadataMapBundle(node.Name)
		if err != nil {
			// Nothing to delete.
			return nil
		}

		metaBundle.m.Lock()
		metaBundle.Services.Delete(namespace, svc)
		metaBundle.m.Unlock()

		cacheKey := utilcache.BuildAgentKey(metadataMapperCachePrefix, node.Name)

		utilcache.Cache.Set(cacheKey, metaBundle, metadataMapExpire)
	}
	return nil
}

// GetPodMetadataNames is used when the API endpoint of the DCA to get the metadata of a pod is hit.
func GetPodMetadataNames(nodeName, ns, podName string) ([]string, error) {
	var metaList []string
	cacheKey := utilcache.BuildAgentKey(metadataMapperCachePrefix, nodeName)

	metaBundleInterface, found := utilcache.Cache.Get(cacheKey)
	if !found {
		log.Tracef("no metadata was found for the pod %s on node %s", podName, nodeName)
		return nil, nil
	}

	metaBundle, ok := metaBundleInterface.(*MetadataMapperBundle)
	if !ok {
		return nil, fmt.Errorf("invalid cache format for the cacheKey: %s", cacheKey)
	}
	// The list of metadata collected in the metaBundle is extensible and is handled here.
	// If new cluster level tags need to be collected by the agent, only this needs to be modified.
	serviceList, foundServices := metaBundle.ServicesForPod(ns, podName)
	if !foundServices {
		log.Tracef("no cached services list found for the pod %s on the node %s", podName, nodeName)
		return nil, nil
	}
	log.Debugf("CacheKey: %s, with %d services", cacheKey, len(serviceList))
	for _, s := range serviceList {
		metaList = append(metaList, fmt.Sprintf("kube_service:%s", s))
	}

	return metaList, nil
}
