/*
Copyright 2025 Adobe. All rights reserved.
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
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

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

	// Global variables for port-forward management
	prometheusPortForwardCmd *exec.Cmd
	prometheusPort          string
)

// setupPrometheusPortForward starts the Prometheus port-forward and waits for it to be ready
func setupPrometheusPortForward(t *testing.T) error {
	// Determine the correct Prometheus port based on the test environment
	prometheusPort = "30007" // default port for local-test
	kubeconfig := os.Getenv("KUBECONFIG")
	if strings.Contains(kubeconfig, "karpenter") {
		prometheusPort = "30008" // port for local-test-karpenter
	} else if strings.Contains(kubeconfig, "node-labels") {
		prometheusPort = "30009" // port for local-test-node-labels
	}

	// Kill any existing port-forward for this port
	killCmd := exec.Command("pkill", "-f", fmt.Sprintf("kubectl port-forward.*%s", prometheusPort))
	if err := killCmd.Run(); err != nil {
		// Ignore errors as there might not be any process to kill
		t.Logf("Note: No existing port-forward process found to kill: %v", err)
	}

	// Start port-forward for Prometheus
	cmd := exec.Command("kubectl", "port-forward", "-n", "kube-system", "svc/prometheus", 
		fmt.Sprintf("%s:9090", prometheusPort), "--kubeconfig", kubeconfig)
	
	// Redirect output to avoid cluttering test output
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward: %v", err)
	}
	
	prometheusPortForwardCmd = cmd
	t.Logf("Started Prometheus port-forward on port %s", prometheusPort)

	// Wait for port-forward to be ready
	retryCount := 0
	maxRetries := 30
	for retryCount < maxRetries {
		// Check if the port is accessible
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/-/ready", prometheusPort))
		if err == nil && resp.StatusCode == 200 {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Warning: Failed to close response body: %v", closeErr)
			}
			t.Logf("Prometheus port-forward is ready on port %s", prometheusPort)
			return nil
		}
		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Warning: Failed to close response body: %v", closeErr)
			}
		}
		
		time.Sleep(2 * time.Second)
		retryCount++
		t.Logf("Waiting for Prometheus port-forward to be ready... (attempt %d/%d)", retryCount, maxRetries)
	}

	// If we get here, port-forward failed to become ready
	cleanupPrometheusPortForward(t)
	return fmt.Errorf("Prometheus port-forward failed to become ready after %d attempts", maxRetries)
}

// cleanupPrometheusPortForward stops the Prometheus port-forward
func cleanupPrometheusPortForward(t *testing.T) {
	if prometheusPortForwardCmd != nil && prometheusPortForwardCmd.Process != nil {
		t.Logf("Stopping Prometheus port-forward on port %s", prometheusPort)
		if err := prometheusPortForwardCmd.Process.Kill(); err != nil {
			t.Logf("Warning: Failed to kill port-forward process: %v", err)
		}
		if err := prometheusPortForwardCmd.Wait(); err != nil {
			t.Logf("Warning: Failed to wait for port-forward process: %v", err)
		}
		prometheusPortForwardCmd = nil
	}
}

// TestMain sets up and tears down the Prometheus port-forward for all tests
func TestMain(m *testing.M) {
	// Set up port-forward before running tests
	if err := setupPrometheusPortForward(&testing.T{}); err != nil {
		log.Errorf("Failed to setup Prometheus port-forward: %v", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Clean up port-forward after tests
	cleanupPrometheusPortForward(&testing.T{})

	os.Exit(code)
}

func grabMetrics(shredderMetrics []string, t *testing.T) map[string]float64 {
	results := make(map[string]float64)
	warnings := make([]string, 0)

	// First check if Prometheus is accessible with retries
	var err error
	retryCount := 0
	maxRetries := 30 // Reduced retries since port-forward is managed

	for retryCount < maxRetries {
		_, _, err = prometheusQuery("up")
		if err == nil {
			break
		}
		t.Logf("Warning: Prometheus not accessible (attempt %d/%d): %v", retryCount+1, maxRetries, err)
		time.Sleep(time.Second)
		retryCount++
	}

	if err != nil {
		t.Logf("Warning: Prometheus is not accessible after %d retries: %v", maxRetries, err)
		t.Logf("Skipping metrics collection")
		return results
	}

	// If we get here, Prometheus is accessible
	for _, metric := range shredderMetrics {
		value, promWarnings, err := prometheusQuery(metric)
		if err != nil {
			t.Logf("Warning: Failed to query metric %s: %v", metric, err)
			continue
		}
		if len(promWarnings) > 0 {
			warnings = append(warnings, promWarnings...)
		}
		if value != nil {
			// Properly extract float64 from model.Vector
			if vector, ok := value.(model.Vector); ok && len(vector) > 0 {
				results[metric] = float64(vector[0].Value)
			}
		}
	}

	if len(warnings) > 0 {
		t.Logf("Warnings while collecting metrics: %v", warnings)
	}

	t.Logf("Collected metrics: %v", results)
	return results
}

func prometheusQuery(query string) (model.Value, v1.Warnings, error) {
	// Use the global prometheusPort variable
	if prometheusPort == "" {
		return nil, nil, fmt.Errorf("Prometheus port not set - port-forward may not be running")
	}

	// Create a new client for each query to avoid connection reuse issues
	client, err := api.NewClient(api.Config{
		Address: fmt.Sprintf("http://localhost:%s", prometheusPort),
		RoundTripper: api.DefaultRoundTripper,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("error creating Prometheus client: %v", err)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Add retry logic for the query
	var result model.Value
	var warnings v1.Warnings
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		result, warnings, err = v1api.Query(ctx, query, time.Now(), v1.WithTimeout(5*time.Second))
		if err == nil {
			break
		}
		if i < maxRetries-1 {
			time.Sleep(time.Second)
		}
	}

	if err != nil {
		return nil, warnings, fmt.Errorf("error querying Prometheus after %d retries: %v", maxRetries, err)
	}

	return result, warnings, nil
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

	// Print the KUBECONFIG being used
	t.Logf("Using KUBECONFIG: %s", os.Getenv("KUBECONFIG"))

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
		ParkedByLabel:                      "shredder.ethos.adobe.net/parked-by",
		ParkedByValue:                      "k8s-shredder",
	}, false)

	if err != nil {
		log.Fatalf("Failed to setup application context: %s", err)
	}

	// Determine the correct node name based on the test environment
	parkedWorkerNode := "k8s-shredder-test-cluster-worker" // default
	if strings.Contains(os.Getenv("KUBECONFIG"), "karpenter") {
		parkedWorkerNode = "k8s-shredder-test-cluster-karpenter-worker" // In Karpenter test, we use the worker node
	} else if strings.Contains(os.Getenv("KUBECONFIG"), "node-labels") {
		parkedWorkerNode = "k8s-shredder-test-cluster-node-labels-worker"
	}

	coreV1Client := appContext.K8sClient.CoreV1()
	// Get the first available worker node if the specific node is not found
	node, err := coreV1Client.Nodes().Get(appContext.Context, parkedWorkerNode, metav1.GetOptions{})
	if err != nil {
		// Try to get any worker node
		nodes, err := coreV1Client.Nodes().List(appContext.Context, metav1.ListOptions{
			LabelSelector: "node-role.kubernetes.io/worker=",
		})
		if err != nil || len(nodes.Items) == 0 {
			t.Fatalf("Failed to get any worker node: %s", err)
		}
		node = &nodes.Items[0]
		t.Logf("Using worker node: %s", node.Name)
	}

	h := handler.NewHandler(appContext)

	// Wait for node TTL to expire
	expirationTime, err := utils.GetParkedNodeExpiryTime(*node, appContext.Config.ExpiresOnLabel)
	if err != nil {
		t.Fatalf("Failed to get expiration time for the parked node %s: %s", node.Name, err)
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
		t.Fatalf("Failed to get running pods from the parked node %s: %s", node.Name, err)
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
	// Print the KUBECONFIG being used
	t.Logf("Using KUBECONFIG: %s", os.Getenv("KUBECONFIG"))

	// Define the metrics to check
	shredderMetrics := []string{
		"shredder_processed_pods_total",
		"shredder_errors_total",
		"shredder_node_force_to_evict_time",
		"shredder_node_label_nodes_parked_total",
		"shredder_node_label_nodes_parking_failed_total",
		"shredder_node_label_processing_duration_seconds",
		"shredder_node_label_matching_nodes_total",
	}

	// Check if we're running in a node labels test environment
	kubeconfig := os.Getenv("KUBECONFIG")
	if strings.Contains(kubeconfig, "node-labels") {
		// Create app context with node label detection enabled
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
			ParkedByLabel:                      "shredder.ethos.adobe.net/parked-by",
			ParkedByValue:                      "k8s-shredder",
			EnableNodeLabelDetection:           true,
			NodeLabelsToDetect:                 []string{"test-label", "maintenance=scheduled"},
		}, false)

		if err != nil {
			log.Fatalf("Failed to setup application context: %s", err)
		}

		// Process nodes with labels
		err = utils.ProcessNodesWithLabels(appContext.Context, appContext, log.WithField("test", "TestShredderMetrics"))
		if err != nil {
			t.Fatalf("Failed to process nodes with labels: %s", err)
		}
	}

	// Check the metrics
	results := grabMetrics(shredderMetrics, t)

	// Verify that we have at least some metrics
	if len(results) == 0 {
		t.Fatal("No metrics found")
	}

	// Log the metrics we found
	for metric, value := range results {
		t.Logf("Found metric %s: %v", metric, value)
	}
}

func TestArgoRolloutRestartAt(t *testing.T) {
	// Print the KUBECONFIG being used
	t.Logf("Using KUBECONFIG: %s", os.Getenv("KUBECONFIG"))

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
		ParkedByLabel:                      "shredder.ethos.adobe.net/parked-by",
		ParkedByValue:                      "k8s-shredder",
	}, false)

	if err != nil {
		log.Fatalf("Failed to setup application context: %s", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  appContext.Config.ArgoRolloutsAPIVersion,
		Resource: "rollouts",
	}

	// Wait for up to 2 minutes for the restartAt field to be set
	timeout := time.After(2 * time.Minute)
	tick := time.Tick(5 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for Argo Rollout restartAt field to be set")
		case <-tick:
			rollout, err := appContext.DynamicK8SClient.Resource(gvr).Namespace("ns-team-k8s-shredder-test").Get(appContext.Context, "test-app-argo-rollout", metav1.GetOptions{})
			if err != nil {
				log.Fatalf("Failed to get the Argo Rollout object: %s", err)
			}
			restartAt, found, err := unstructured.NestedString(rollout.Object, "spec", "restartAt")
			if err != nil {
				log.Fatalf("Failed to get the Argo Rollout spec.restartAt field: %s", err)
			}
			if found && restartAt != "" {
				t.Logf("Found restartAt field set to: %s", restartAt)
				return
			}
			t.Log("Waiting for restartAt field to be set...")
		}
	}
}

// TestKarpenterMetrics validates Karpenter metrics when drift detection is enabled
func TestKarpenterMetrics(t *testing.T) {
	// Print the KUBECONFIG being used
	t.Logf("Using KUBECONFIG: %s", os.Getenv("KUBECONFIG"))

	// Check if we're running in a Karpenter test environment
	kubeconfig := os.Getenv("KUBECONFIG")
	if !strings.Contains(kubeconfig, "karpenter") {
		t.Skip("Skipping Karpenter metrics test: not running in a Karpenter test environment")
	}

	// Define the Karpenter metrics to check
	karpenterMetrics := []string{
		"shredder_karpenter_drifted_nodes_total",
		"shredder_karpenter_nodes_parked_total",
		"shredder_karpenter_nodes_parking_failed_total",
		"shredder_karpenter_processing_duration_seconds",
		"shredder_nodes_parked_total",
		"shredder_nodes_parking_failed_total",
		"shredder_processing_duration_seconds",
	}

	// Only read the metrics, do not mutate cluster state
	results := grabMetrics(karpenterMetrics, t)

	if len(results) == 0 {
		t.Fatal("No Karpenter metrics found")
	}

	t.Logf("Karpenter metrics found:")
	for metric, value := range results {
		t.Logf("Found metric %s: %v", metric, value)
	}

	if _, ok := results["shredder_karpenter_drifted_nodes_total"]; !ok {
		t.Error("Missing shredder_karpenter_drifted_nodes_total metric")
	}
	if _, ok := results["shredder_karpenter_nodes_parked_total"]; !ok {
		t.Error("Missing shredder_karpenter_nodes_parked_total metric")
	}
	if _, ok := results["shredder_karpenter_nodes_parking_failed_total"]; !ok {
		t.Error("Missing shredder_karpenter_nodes_parking_failed_total metric")
	}
	if _, ok := results["shredder_karpenter_processing_duration_seconds"]; !ok {
		t.Error("Missing shredder_karpenter_processing_duration_seconds metric")
	}

	if driftedTotal, ok := results["shredder_karpenter_drifted_nodes_total"]; ok {
		t.Logf("Total drifted nodes detected (historical): %v", driftedTotal)
	} else {
		t.Error("Missing shredder_karpenter_drifted_nodes_total metric")
	}
	if parkedTotal, ok := results["shredder_karpenter_nodes_parked_total"]; ok {
		t.Logf("Total nodes parked (historical): %v", parkedTotal)
	} else {
		t.Error("Missing shredder_karpenter_nodes_parked_total metric")
	}

	t.Log("Karpenter metrics test completed successfully")
}

// TestNodeLabelMetrics specifically tests the node label detection metrics
func TestNodeLabelMetrics(t *testing.T) {
	// Print the KUBECONFIG being used
	t.Logf("Using KUBECONFIG: %s", os.Getenv("KUBECONFIG"))

	// Check if we're running in a node labels test environment
	kubeconfig := os.Getenv("KUBECONFIG")
	if !strings.Contains(kubeconfig, "node-labels") {
		t.Skip("Skipping node label metrics test: not running in a node labels test environment")
	}

	// Define the node label metrics to check
	nodeLabelMetrics := []string{
		"shredder_node_label_nodes_parked_total",
		"shredder_node_label_nodes_parking_failed_total",
		"shredder_node_label_processing_duration_seconds",
		"shredder_node_label_matching_nodes_total",
		"shredder_nodes_parked_total",
		"shredder_nodes_parking_failed_total",
		"shredder_processing_duration_seconds",
	}

	// Only read the metrics, do not mutate cluster state
	results := grabMetrics(nodeLabelMetrics, t)

	if len(results) == 0 {
		t.Fatal("No node label metrics found")
	}

	t.Logf("Node label metrics found:")
	for metric, value := range results {
		t.Logf("Found metric %s: %v", metric, value)
	}

	if _, ok := results["shredder_node_label_nodes_parked_total"]; !ok {
		t.Error("Missing shredder_node_label_nodes_parked_total metric")
	}
	if _, ok := results["shredder_node_label_matching_nodes_total"]; !ok {
		t.Error("Missing shredder_node_label_matching_nodes_total metric")
	}
	if _, ok := results["shredder_node_label_nodes_parking_failed_total"]; !ok {
		t.Error("Missing shredder_node_label_nodes_parking_failed_total metric")
	}
	if _, ok := results["shredder_node_label_processing_duration_seconds"]; !ok {
		t.Error("Missing shredder_node_label_processing_duration_seconds metric")
	}

	if parkedTotal, ok := results["shredder_node_label_nodes_parked_total"]; ok {
		t.Logf("Total nodes parked (historical): %v", parkedTotal)
	} else {
		t.Error("Missing shredder_node_label_nodes_parked_total metric")
	}
	if matchingNodes, ok := results["shredder_node_label_matching_nodes_total"]; ok {
		t.Logf("Total matching nodes (current): %v", matchingNodes)
	} else {
		t.Error("Missing shredder_node_label_matching_nodes_total metric")
	}

	t.Log("Node label metrics test completed successfully")
}
