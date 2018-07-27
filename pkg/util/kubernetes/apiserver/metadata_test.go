// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2018 Datadog, Inc.

// +build kubeapiserver

package apiserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DataDog/datadog-agent/pkg/util/cache"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func alwaysReady() bool { return true }

func TestMetadataControllerMapServices(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	metaController, informerFactory := newFakeMetadataController(clientset)

	stop := make(chan struct{})
	defer close(stop)
	informerFactory.Start(stop)
	go metaController.Run(stop)

	pod1 := newFakePod(
		"foo",
		"pod1_name",
		"1111",
		"1.1.1.1",
	)

	pod2 := newFakePod(
		"foo",
		"pod2_name",
		"2222",
		"2.2.2.2",
	)

	tests := []struct {
		desc            string
		endpoints       []*v1.Endpoints
		expectedBundles map[string]ServicesMapper
	}{
		{
			"one service on multiple nodes",
			[]*v1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
					Subsets: []v1.EndpointSubset{
						{
							Addresses: []v1.EndpointAddress{
								newFakeEndpointAddress("node1", pod1),
								newFakeEndpointAddress("node2", pod2),
							},
						},
					},
				},
			},
			map[string]ServicesMapper{
				"node1": ServicesMapper{
					"foo": {
						"pod1_name": sets.NewString("svc1"),
					},
				},
				"node2": ServicesMapper{
					"foo": {
						"pod2_name": sets.NewString("svc1"),
					},
				},
			},
		},
		{
			"pod added to existing service",
			[]*v1.Endpoints{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
					Subsets: []v1.EndpointSubset{
						{
							Addresses: []v1.EndpointAddress{
								newFakeEndpointAddress("node1", pod1),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "svc1"},
					Subsets: []v1.EndpointSubset{
						{
							Addresses: []v1.EndpointAddress{
								newFakeEndpointAddress("node1", pod1),
								newFakeEndpointAddress("node1", pod2),
							},
						},
					},
				},
			},
			map[string]ServicesMapper{
				"node1": ServicesMapper{
					"foo": {
						"pod1_name": sets.NewString("svc1"),
						"pod2_name": sets.NewString("svc1"),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		for _, endpoints := range tt.endpoints {
			err := metaController.mapServices(endpoints)
			require.NoError(t, err)
		}

		for nodeName, expectedMapper := range tt.expectedBundles {
			cacheKey := cache.BuildAgentKey(metadataMapperCachePrefix, nodeName)
			v, ok := cache.Cache.Get(cacheKey)
			require.True(t, ok)
			metaBundle, ok := v.(*MetadataMapperBundle)
			require.True(t, ok)

			assert.Equal(t, expectedMapper, metaBundle.Services)
		}
	}
}

func newFakeMetadataController(client kubernetes.Interface) (*MetadataController, informers.SharedInformerFactory) {
	informerFactory := informers.NewSharedInformerFactory(client, 0*time.Second)

	metaController := NewMetadataController(
		informerFactory.Core().V1().Nodes(),
		informerFactory.Core().V1().Endpoints(),
	)
	metaController.endpointsListerSynced = alwaysReady

	return metaController, informerFactory
}
