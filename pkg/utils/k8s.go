/*
Copyright 2022 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package utils

import (
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"strconv"
	"time"
)

func getK8SClient() (*kubernetes.Clientset, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func getDynamicK8SClient() (*dynamic.DynamicClient, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	// Create a dynamic client
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, errors.Errorf("Error creating dynamic client: %v", err)
	}

	return dynamicClient, nil
}

// NodeHasTaint check if a node has a taint set
func NodeHasTaint(node v1.Node, key string) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == key {
			return true
		}
	}
	return false
}

// NodeHasLabel check if a node has a specific label set
func NodeHasLabel(node v1.Node, key string) bool {
	for k := range node.Labels {
		if k == key {
			return true
		}
	}
	return false
}

// PodEvictionAllowed check if a pod has the `skipEvictionLabel`=false label set
func PodEvictionAllowed(pod v1.Pod, skipEvictionLabel string) bool {
	if PodHasLabel(pod, skipEvictionLabel) {
		if pod.Labels[skipEvictionLabel] == "false" {
			return false
		}
	}
	return true
}

// PodHasLabel check if a pod has a specific label set
func PodHasLabel(pod v1.Pod, key string) bool {
	for k := range pod.Labels {
		if k == key {
			return true
		}
	}
	return false
}

// GetParkedNodeExpiryTime get the time a parked node TTL expires
func GetParkedNodeExpiryTime(node v1.Node, expiresOnLabel string) (time.Time, error) {
	i, err := strconv.ParseFloat(node.Labels[expiresOnLabel], 64)
	if err != nil {
		return time.Now().UTC(), errors.Errorf("Failed to parse label %s with value %s", expiresOnLabel, node.Labels[expiresOnLabel])
	}
	return time.Unix(int64(i), 0).UTC(), nil
}
