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

package utils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLimitNodesToPark_NoLimit(t *testing.T) {
	// Test case: MaxParkedNodes = "0" (no limit)
	// Even with no limit, nodes should be sorted by creation time (oldest first)
	cfg := config.Config{
		MaxParkedNodes:     "0",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create nodes with different creation times (node3 is oldest, node1 is newest)
	baseTime := time.Now()
	node3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node3",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-3 * time.Hour)}, // Oldest
		},
	}
	node2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node2",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-2 * time.Hour)},
		},
	}
	node1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node1",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-1 * time.Hour)}, // Newest
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), node2, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), node3, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Add some parked nodes to simulate existing parked nodes
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Pass nodes in arbitrary order - they should be sorted by creation time
	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
		{Name: "node3"},
	}

	logger := log.WithField("test", "TestLimitNodesToPark_NoLimit")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	assert.Equal(t, 3, len(result), "Should return all nodes when no limit")
	// Verify nodes are sorted by creation time (oldest first)
	assert.Equal(t, "node3", result[0].Name, "Oldest node should be first")
	assert.Equal(t, "node2", result[1].Name, "Middle node should be second")
	assert.Equal(t, "node1", result[2].Name, "Newest node should be last")
}

func TestLimitNodesToPark_WithLimit(t *testing.T) {
	// Test case: MaxParkedNodes = "2", 1 already parked, 3 eligible nodes
	// Nodes should be sorted by creation time (oldest first)
	cfg := config.Config{
		MaxParkedNodes:     "2",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create nodes with different creation times (node3 is oldest, node1 is newest)
	baseTime := time.Now()
	node3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node3",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-3 * time.Hour)}, // Oldest
		},
	}
	node2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node2",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-2 * time.Hour)},
		},
	}
	node1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node1",
			CreationTimestamp: metav1.Time{Time: baseTime.Add(-1 * time.Hour)}, // Newest
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), node2, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), node3, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Add one parked node to simulate existing parked nodes
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Pass nodes in arbitrary order - they should be sorted by creation time
	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
		{Name: "node3"},
	}

	logger := log.WithField("test", "TestLimitNodesToPark_WithLimit")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// Should only park 1 node (2 max - 1 already parked = 1 available slot)
	assert.Equal(t, 1, len(result))
	// Should park node3 as it's the oldest
	assert.Equal(t, "node3", result[0].Name)
}

func TestLimitNodesToPark_NoAvailableSlots(t *testing.T) {
	// Test case: MaxParkedNodes = "2", 2 already parked, 3 eligible nodes
	cfg := config.Config{
		MaxParkedNodes:     "2",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
		{Name: "node3"},
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Add two parked nodes to simulate existing parked nodes
	parkedNode1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node-1",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	parkedNode2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node-2",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode2, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestLimitNodesToPark_NoAvailableSlots")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// Should park no nodes (no available slots)
	assert.Equal(t, 0, len(result))
}

func TestLimitNodesToPark_NegativeLimit(t *testing.T) {
	// Test case: MaxParkedNodes = "-1" (invalid, should be treated as 0)
	cfg := config.Config{
		MaxParkedNodes:     "-1",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	logger := log.WithField("test", "TestLimitNodesToPark_NegativeLimit")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// Should park all nodes (negative limit treated as no limit)
	assert.Equal(t, len(nodes), len(result))
	assert.Equal(t, nodes, result)
}

func TestLimitNodesToPark_PercentageLimit(t *testing.T) {
	// Test case: MaxParkedNodes = "20%", 10 total nodes, 1 already parked, 3 eligible nodes
	// Expected: 20% of 10 = 2 nodes max, 1 already parked, so 1 more can be parked
	cfg := config.Config{
		MaxParkedNodes:     "20%",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
		{Name: "node3"},
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Add 10 total nodes to the cluster
	for i := 1; i <= 10; i++ {
		nodeName := fmt.Sprintf("cluster-node-%d", i)
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	// Add one parked node to simulate existing parked nodes
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestLimitNodesToPark_PercentageLimit")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// 11 total nodes (10 cluster + 1 parked), 20% = 2.2 -> floor to 2
	// 1 already parked, so should park 1 more
	assert.Equal(t, 1, len(result))
	assert.Equal(t, "node1", result[0].Name)
}

func TestLimitNodesToPark_PercentageLimit_NoSlots(t *testing.T) {
	// Test case: MaxParkedNodes = "10%", 10 total nodes (10% = 1), 1 already parked
	// Expected: No slots available
	cfg := config.Config{
		MaxParkedNodes:     "10%",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
	}

	// Create a fake k8s client with 10 nodes
	fakeClient := fake.NewSimpleClientset()

	for i := 1; i <= 9; i++ {
		nodeName := fmt.Sprintf("cluster-node-%d", i)
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}
		_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	// Add one parked node (total = 10)
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "already-parked-node",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
			},
		},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestLimitNodesToPark_PercentageLimit_NoSlots")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// 10 total nodes, 10% = 1, 1 already parked, no slots available
	assert.Equal(t, 0, len(result))
}

func TestLimitNodesToPark_SortingByAge(t *testing.T) {
	// Test case: Verify nodes are sorted by creation time (oldest first)
	cfg := config.Config{
		MaxParkedNodes:     "2",
		UpgradeStatusLabel: "test-upgrade-status",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create 5 nodes with different creation times
	baseTime := time.Now()
	nodeCreationTimes := map[string]time.Time{
		"node-newest":      baseTime.Add(-1 * time.Hour),
		"node-older":       baseTime.Add(-2 * time.Hour),
		"node-middle":      baseTime.Add(-3 * time.Hour),
		"node-very-old":    baseTime.Add(-5 * time.Hour),
		"node-oldest-ever": baseTime.Add(-10 * time.Hour), // Should be parked first
	}

	for name, createTime := range nodeCreationTimes {
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: metav1.Time{Time: createTime},
			},
		}
		_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	// Pass nodes in arbitrary order
	nodes := []NodeInfo{
		{Name: "node-newest"},
		{Name: "node-middle"},
		{Name: "node-oldest-ever"},
		{Name: "node-older"},
		{Name: "node-very-old"},
	}

	logger := log.WithField("test", "TestLimitNodesToPark_SortingByAge")

	result, err := LimitNodesToPark(context.Background(), fakeClient, nodes, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)

	assert.NoError(t, err)
	// Should park 2 oldest nodes
	assert.Equal(t, 2, len(result))
	// Should be sorted oldest first
	assert.Equal(t, "node-oldest-ever", result[0].Name, "Oldest node should be first")
	assert.Equal(t, "node-very-old", result[1].Name, "Second oldest node should be second")
}

func TestCountParkedNodes(t *testing.T) {
	// Test case: Count parked nodes
	upgradeStatusLabel := "test-upgrade-status"

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Add some nodes with different statuses
	parkedNode1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node-1",
			Labels: map[string]string{
				upgradeStatusLabel: "parked",
			},
		},
	}
	parkedNode2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node-2",
			Labels: map[string]string{
				upgradeStatusLabel: "parked",
			},
		},
	}
	normalNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-node",
			Labels: map[string]string{
				upgradeStatusLabel: "normal",
			},
		},
	}
	noLabelNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "no-label-node",
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode2, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), normalNode, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), noLabelNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestCountParkedNodes")

	count, err := CountParkedNodes(context.Background(), fakeClient, upgradeStatusLabel, logger)

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

// TestParkNodes tests the main parking functionality
func TestParkNodes(t *testing.T) {
	// Test case: Park multiple nodes with extra labels
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
		ParkedNodeTaint:    "test-upgrade-status=parked:NoSchedule",
		ParkedNodeTTL:      1 * time.Hour,
		ExtraParkingLabels: map[string]string{
			"example.com/owner": "infrastructure",
			"example.com/batch": "batch-1",
		},
	}

	nodes := []NodeInfo{
		{Name: "node1"},
		{Name: "node2"},
	}

	// Create a fake k8s client with existing nodes
	fakeClient := fake.NewSimpleClientset()

	// Create test nodes
	node1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
		},
		Spec: v1.NodeSpec{
			Unschedulable: false,
		},
	}
	node2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node2",
		},
		Spec: v1.NodeSpec{
			Unschedulable: false,
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Nodes().Create(context.Background(), node2, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create test pods on the nodes
	pod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "node1",
		},
	}
	pod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "node2",
		},
	}

	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), pod1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), pod2, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestParkNodes")

	// Test dry-run mode
	err = ParkNodes(context.Background(), fakeClient, nodes, cfg, true, "test", logger)
	assert.NoError(t, err)

	// Verify nodes are not actually modified in dry-run mode
	node1After, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "node1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Empty(t, node1After.Labels)
	assert.False(t, node1After.Spec.Unschedulable)

	// Test real execution
	err = ParkNodes(context.Background(), fakeClient, nodes, cfg, false, "test", logger)
	assert.NoError(t, err)

	// Verify nodes are properly parked
	node1After, err = fakeClient.CoreV1().Nodes().Get(context.Background(), "node1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "parked", node1After.Labels["test-upgrade-status"])
	assert.Equal(t, "k8s-shredder", node1After.Labels["test-parked-by"])
	assert.Equal(t, "infrastructure", node1After.Labels["example.com/owner"])
	assert.Equal(t, "batch-1", node1After.Labels["example.com/batch"])
	assert.True(t, node1After.Spec.Unschedulable)
	assert.Len(t, node1After.Spec.Taints, 1)
	assert.Equal(t, "test-upgrade-status", node1After.Spec.Taints[0].Key)
	assert.Equal(t, "parked", node1After.Spec.Taints[0].Value)
	assert.Equal(t, v1.TaintEffectNoSchedule, node1After.Spec.Taints[0].Effect)

	// Verify pods are labeled
	pod1After, err := fakeClient.CoreV1().Pods("default").Get(context.Background(), "pod1", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "parked", pod1After.Labels["test-upgrade-status"])
	assert.Equal(t, "k8s-shredder", pod1After.Labels["test-parked-by"])
	assert.Equal(t, "infrastructure", pod1After.Labels["example.com/owner"])
	assert.Equal(t, "batch-1", pod1After.Labels["example.com/batch"])
}

// TestParkNodes_EmptyNodes tests parking with no nodes
func TestParkNodes_EmptyNodes(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
		ParkedNodeTTL:      1 * time.Hour,
	}

	fakeClient := fake.NewSimpleClientset()
	logger := log.WithField("test", "TestParkNodes_EmptyNodes")

	err := ParkNodes(context.Background(), fakeClient, []NodeInfo{}, cfg, false, "test", logger)
	assert.NoError(t, err)
}

// TestParkNodes_NodeWithNoName tests parking with invalid node info
func TestParkNodes_NodeWithNoName(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
		ParkedNodeTTL:      1 * time.Hour,
	}

	nodes := []NodeInfo{
		{Name: ""}, // Invalid node with no name
		{Name: "valid-node"},
	}

	fakeClient := fake.NewSimpleClientset()

	// Create valid node
	validNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "valid-node",
		},
	}
	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), validNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestParkNodes_NodeWithNoName")

	err = ParkNodes(context.Background(), fakeClient, nodes, cfg, false, "test", logger)
	assert.NoError(t, err)

	// Verify only the valid node was processed
	validNodeAfter, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "valid-node", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "parked", validNodeAfter.Labels["test-upgrade-status"])
}

// TestUnparkNode tests the node unparking functionality
func TestUnparkNode(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
		ParkedNodeTaint:    "test-upgrade-status=parked:NoSchedule",
		ExtraParkingLabels: map[string]string{
			"example.com/owner": "infrastructure",
		},
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a parked node
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
				"test-expires-on":     "1234567890",
				"test-parked-by":      "k8s-shredder",
				"example.com/owner":   "infrastructure",
			},
		},
		Spec: v1.NodeSpec{
			Unschedulable: true,
			Taints: []v1.Taint{
				{
					Key:    "test-upgrade-status",
					Value:  "parked",
					Effect: v1.TaintEffectNoSchedule,
				},
			},
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create a parked pod on the node
	parkedPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parked-pod",
			Namespace: "default",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
				"test-expires-on":     "1234567890",
				"test-parked-by":      "k8s-shredder",
				"example.com/owner":   "infrastructure",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "parked-node",
		},
	}

	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), parkedPod, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestUnparkNode")

	// Test dry-run mode
	err = UnparkNode(context.Background(), fakeClient, "parked-node", cfg, true, logger)
	assert.NoError(t, err)

	// Verify node is not actually modified in dry-run mode
	nodeAfterDryRun, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "parked-node", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "parked", nodeAfterDryRun.Labels["test-upgrade-status"])
	assert.True(t, nodeAfterDryRun.Spec.Unschedulable)

	// Test real execution
	err = UnparkNode(context.Background(), fakeClient, "parked-node", cfg, false, logger)
	assert.NoError(t, err)

	// Verify node is properly unparked
	nodeAfter, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "parked-node", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "unparked", nodeAfter.Labels["test-upgrade-status"])
	assert.Equal(t, "k8s-shredder", nodeAfter.Labels["test-parked-by"])
	assert.Empty(t, nodeAfter.Labels["test-expires-on"])
	assert.Empty(t, nodeAfter.Labels["example.com/owner"])
	assert.False(t, nodeAfter.Spec.Unschedulable)
	assert.Empty(t, nodeAfter.Spec.Taints)

	// Verify pod is unparked
	podAfter, err := fakeClient.CoreV1().Pods("default").Get(context.Background(), "parked-pod", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "unparked", podAfter.Labels["test-upgrade-status"])
	assert.Equal(t, "k8s-shredder", podAfter.Labels["test-parked-by"])
	assert.Empty(t, podAfter.Labels["test-expires-on"])
	assert.Empty(t, podAfter.Labels["example.com/owner"])
}

// TestUnparkNode_NotParked tests unparking a node that's not parked
func TestUnparkNode_NotParked(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a normal node (not parked)
	normalNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "normal-node",
			Labels: map[string]string{
				"test-upgrade-status": "normal",
			},
		},
		Spec: v1.NodeSpec{
			Unschedulable: false,
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), normalNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestUnparkNode_NotParked")

	// Test unparking a non-parked node
	err = UnparkNode(context.Background(), fakeClient, "normal-node", cfg, false, logger)
	assert.NoError(t, err)

	// Verify node is unchanged
	nodeAfter, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "normal-node", metav1.GetOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "normal", nodeAfter.Labels["test-upgrade-status"])
	assert.False(t, nodeAfter.Spec.Unschedulable)
}

// TestUnparkNode_NodeNotFound tests unparking a non-existent node
func TestUnparkNode_NodeNotFound(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	fakeClient := fake.NewSimpleClientset()
	logger := log.WithField("test", "TestUnparkNode_NodeNotFound")

	err := UnparkNode(context.Background(), fakeClient, "non-existent-node", cfg, false, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get node")
}

// TestCheckPodParkingSafety_Safe tests safety check when all pods are properly labeled
func TestCheckPodParkingSafety_Safe(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a parked node
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node",
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create properly labeled pods
	safePod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "safe-pod-1",
			Namespace: "default",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
				"test-expires-on":     "1234567890",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "parked-node",
		},
	}

	safePod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "safe-pod-2",
			Namespace: "default",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
				"test-expires-on":     "1234567890",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "parked-node",
		},
	}

	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), safePod1, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), safePod2, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestCheckPodParkingSafety_Safe")

	// Test safety check
	safe, err := CheckPodParkingSafety(context.Background(), fakeClient, "parked-node", cfg, logger)
	assert.NoError(t, err)
	assert.True(t, safe)
}

// TestCheckPodParkingSafety_Unsafe tests safety check when pods are missing required labels
func TestCheckPodParkingSafety_Unsafe(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a parked node
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node",
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create unsafe pod (missing required labels)
	unsafePod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unsafe-pod",
			Namespace: "default",
			Labels: map[string]string{
				"test-upgrade-status": "parked",
				// Missing ExpiresOnLabel
			},
		},
		Spec: v1.PodSpec{
			NodeName: "parked-node",
		},
	}

	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), unsafePod, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestCheckPodParkingSafety_Unsafe")

	// Test safety check
	safe, err := CheckPodParkingSafety(context.Background(), fakeClient, "parked-node", cfg, logger)
	assert.NoError(t, err)
	assert.False(t, safe)
}

// TestCheckPodParkingSafety_NoLabels tests safety check when pod has no labels
func TestCheckPodParkingSafety_NoLabels(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a parked node
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node",
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Create pod with no labels
	noLabelPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-label-pod",
			Namespace: "default",
			// No labels at all
		},
		Spec: v1.PodSpec{
			NodeName: "parked-node",
		},
	}

	_, err = fakeClient.CoreV1().Pods("default").Create(context.Background(), noLabelPod, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestCheckPodParkingSafety_NoLabels")

	// Test safety check
	safe, err := CheckPodParkingSafety(context.Background(), fakeClient, "parked-node", cfg, logger)
	assert.NoError(t, err)
	assert.False(t, safe)
}

// TestCheckPodParkingSafety_NoPods tests safety check when node has no eligible pods
func TestCheckPodParkingSafety_NoPods(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	// Create a fake k8s client
	fakeClient := fake.NewSimpleClientset()

	// Create a parked node with no pods
	parkedNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "parked-node",
		},
	}

	_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), parkedNode, metav1.CreateOptions{})
	assert.NoError(t, err)

	logger := log.WithField("test", "TestCheckPodParkingSafety_NoPods")

	// Test safety check
	safe, err := CheckPodParkingSafety(context.Background(), fakeClient, "parked-node", cfg, logger)
	assert.NoError(t, err)
	assert.True(t, safe) // No eligible pods means safety check passes (only DaemonSet/static pods remain)
}

// TestCheckPodParkingSafety_NodeNotFound tests safety check with non-existent node
func TestCheckPodParkingSafety_NodeNotFound(t *testing.T) {
	cfg := config.Config{
		UpgradeStatusLabel: "test-upgrade-status",
		ExpiresOnLabel:     "test-expires-on",
		ParkedByLabel:      "test-parked-by",
		ParkedByValue:      "k8s-shredder",
	}

	fakeClient := fake.NewSimpleClientset()
	logger := log.WithField("test", "TestCheckPodParkingSafety_NodeNotFound")

	// Test safety check with non-existent node
	// When a node doesn't exist, getEligiblePodsForNode returns an empty list, not an error
	safe, err := CheckPodParkingSafety(context.Background(), fakeClient, "non-existent-node", cfg, logger)
	assert.NoError(t, err)
	assert.True(t, safe) // No eligible pods means safety check passes (only DaemonSet/static pods remain)
}
