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

package e2e

import (
	"context"
	"fmt"
	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/adobe/k8s-shredder/pkg/handler"
	"github.com/adobe/k8s-shredder/pkg/utils"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	// Intentionally skipped the gauge metrics as they are going to be wiped out before every eviction loop
	shredderMetrics = []string{
		"shredder_loops_total",
		"shredder_loops_duration_seconds",
		"shredder_processed_nodes_total",
		"shredder_processed_pods_total",
		"shredder_errors_total",
	}

	// Intentionally skipped the gauge metrics as they are going to be wiped out before every eviction loop
	shredderGaugeMetrics = []string{
		"shredder_pod_errors_total",
		"shredder_node_force_to_evict_time",
		"shredder_pod_force_to_evict_time",
	}
)

func grabMetrics(shredderMetrics []string, t *testing.T) []string {
	results := make([]string, 0)
	warnings := make([]string, 0)

	for _, shredderMetric := range shredderMetrics {
		result, warning, err := prometheusQuery(shredderMetric)
		if err != nil {
			t.Errorf("Error querying Prometheus: %v\n", err)
		}
		warnings = append(warnings, warning...)
		results = append(results, result.String())
	}

	if len(warnings) > 0 {
		t.Logf("Warnings: %v\n", strings.Join(warnings, "\n"))
	}

	t.Logf("Results: \n%v\n", strings.Join(results, "\n"))

	return results
}

func prometheusQuery(query string) (model.Value, v1.Warnings, error) {

	client, err := api.NewClient(api.Config{
		Address: "http://localhost:30007",
	})
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return v1api.Query(ctx, query, time.Now(), v1.WithTimeout(5*time.Second))
}

func compareTime(expirationTime time.Time, t *testing.T, ch chan time.Time) {
	currentTime := time.Now().UTC()

	for !currentTime.After(expirationTime.UTC()) {
		t.Logf("Node TTL haven't expired yet: current time(UTC): %s, expire time(UTC): %s", currentTime, expirationTime.UTC())
		time.Sleep(10 * time.Second)
		currentTime = time.Now().UTC()

	}
	ch <- currentTime
}

// Validates that k8s-shredder cleanup a parked node after its TTL expires
func TestNodeIsCleanedUp(t *testing.T) {
	var err error

	appContext, err := utils.NewAppContext(config.Config{
		ParkedNodeTTL:                      30 * time.Second,
		EvictionLoopInterval:               10 * time.Second,
		RollingRestartThreshold:            0.1,
		UpgradeStatusLabel:                 "shredder.ethos.adobe.net/upgrade-status",
		ExpiresOnLabel:                     "shredder.ethos.adobe.net/parked-node-expires-on",
		NamespacePrefixSkipInitialEviction: "",
		RestartedAtAnnotation:              "shredder.ethos.adobe.net/restartedAt",
		AllowEvictionLabel:                 "shredder.ethos.adobe.net/allow-eviction",
		ToBeDeletedTaint:                   "ToBeDeletedByClusterAutoscaler",
	}, false)

	if err != nil {
		log.Fatalf("Failed to setup application context: %s", err)
	}

	parkedWorkerNode := "k8s-shredder-test-cluster-worker"
	coreV1Client := appContext.K8sClient.CoreV1()
	// k8s-shredder-test-cluster-worker worker node is tainted in local_env_prep.sh
	node, err := coreV1Client.Nodes().Get(appContext.Context, parkedWorkerNode, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get the parked node: %s", err)
	}

	h := handler.NewHandler(appContext)

	// Wait for node TTL to expire
	expirationTime, err := utils.GetParkedNodeExpiryTime(*node, appContext.Config.ExpiresOnLabel)
	if err != nil {
		t.Fatalf("Failed to get expiration time for the parked node %s: %s", parkedWorkerNode, err)
	}
	ch := make(chan time.Time)
	go func() {
		compareTime(expirationTime, t, ch)
	}()
	currentTime := <-ch

	t.Logf("Node TTL expired: current time(UTC): %s, expire time(UTC): %s", currentTime, expirationTime.UTC())

	grabMetrics(append(shredderMetrics, shredderGaugeMetrics...), t)

	// Sleep a bit to let the apiserver catch up
	time.Sleep(10 * time.Second)

	pods, err := h.GetPodsForNode(*node)

	if err != nil {
		t.Fatalf("Failed to get running pods from the parked node %s: %s", parkedWorkerNode, err)
	}

	userNamespaces := []string{"ns-team-k8s-shredder-test", "ns-k8s-shredder-test"}
	for _, pod := range pods {
		if pod.DeletionTimestamp == nil && slices.Contains(userNamespaces, pod.Namespace) {
			t.Logf("Pod %s was not evicted by shredder", pod.Name)
			t.Fail()
		}
	}
}

// Validates shredder metrics
func TestShredderMetrics(t *testing.T) {

	// Intentionally skipped the gauge metrics as they are going to be wiped out before every eviction loop
	shredderMetrics := []string{
		"shredder_loops_total",
		"shredder_loops_duration_seconds",
		"shredder_processed_nodes_total",
		"shredder_processed_pods_total",
		"shredder_errors_total",
	}

	results := grabMetrics(shredderMetrics, t)

	if len(results) == len(shredderMetrics) {
		t.Log("Metrics validation test passed!")
	}
}

func TestArgoRolloutRestartAt(t *testing.T) {
	var err error

	appContext, err := utils.NewAppContext(config.Config{
		ParkedNodeTTL:                      30 * time.Second,
		EvictionLoopInterval:               10 * time.Second,
		RollingRestartThreshold:            0.1,
		UpgradeStatusLabel:                 "shredder.ethos.adobe.net/upgrade-status",
		ExpiresOnLabel:                     "shredder.ethos.adobe.net/parked-node-expires-on",
		NamespacePrefixSkipInitialEviction: "",
		RestartedAtAnnotation:              "shredder.ethos.adobe.net/restartedAt",
		AllowEvictionLabel:                 "shredder.ethos.adobe.net/allow-eviction",
		ToBeDeletedTaint:                   "ToBeDeletedByClusterAutoscaler",
		ArgoRolloutsAPIVersion:             "v1alpha1",
	}, false)

	if err != nil {
		log.Fatalf("Failed to setup application context: %s", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  appContext.Config.ArgoRolloutsAPIVersion,
		Resource: "rollouts",
	}

	rollout, err := appContext.DynamicK8SClient.Resource(gvr).Namespace("ns-team-k8s-shredder-test").Get(appContext.Context, "test-app-argo-rollout", metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Failed to get the Argo Rollout object: %s", err)
	}
	_, found, err := unstructured.NestedString(rollout.Object, "spec", "restartAt")

	if err != nil {
		log.Fatalf("Failed to get the Argo Rollout spec.restartAt field: %s", err)
	}

	if !found {
		t.Fatalf("Argo Rollout object does not have the spec.restartAt field set")
	}
}
