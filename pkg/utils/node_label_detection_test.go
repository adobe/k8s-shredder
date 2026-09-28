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
	"testing"

	"github.com/adobe/k8s-shredder/pkg/config"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestParseLabelSelector tests the parseLabelSelector function
func TestParseLabelSelector(t *testing.T) {
	tests := []struct {
		name             string
		selector         string
		expectedKey      string
		expectedValue    string
		expectedHasValue bool
	}{
		{
			name:             "Key only selector",
			selector:         "app",
			expectedKey:      "app",
			expectedValue:    "",
			expectedHasValue: false,
		},
		{
			name:             "Key value selector",
			selector:         "app=web",
			expectedKey:      "app",
			expectedValue:    "web",
			expectedHasValue: true,
		},
		{
			name:             "Key value selector with equals in value",
			selector:         "app=web=frontend",
			expectedKey:      "app",
			expectedValue:    "web=frontend",
			expectedHasValue: true,
		},
		{
			name:             "Empty selector",
			selector:         "",
			expectedKey:      "",
			expectedValue:    "",
			expectedHasValue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			key, value, hasValue := parseLabelSelector(tt.selector, logger)

			assert.Equal(t, tt.expectedKey, key)
			assert.Equal(t, tt.expectedValue, value)
			assert.Equal(t, tt.expectedHasValue, hasValue)
		})
	}
}

// TestNodeMatchesLabelSelectors tests the nodeMatchesLabelSelectors function
func TestNodeMatchesLabelSelectors(t *testing.T) {
	tests := []struct {
		name               string
		node               *v1.Node
		labelSelectors     []string
		upgradeStatusLabel string
		expected           bool
		description        string
	}{
		{
			name: "Node matches key-only selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "upgrade-status",
			expected:           true,
			description:        "Node with matching key should return true",
		},
		{
			name: "Node matches key=value selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			labelSelectors:     []string{"app=web"},
			upgradeStatusLabel: "upgrade-status",
			expected:           true,
			description:        "Node with matching key=value should return true",
		},
		{
			name: "Node doesn't match key=value selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "api",
					},
				},
			},
			labelSelectors:     []string{"app=web"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Node with different value should return false",
		},
		{
			name: "Node doesn't have the key",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Node without the key should return false",
		},
		{
			name: "Node is already parked",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app":            "web",
						"upgrade-status": "parked",
					},
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Already parked node should return false",
		},
		{
			name: "Node has no labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: map[string]string{},
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Node with no labels should return false",
		},
		{
			name: "Node has nil labels",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: nil,
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Node with nil labels should return false",
		},
		{
			name: "Node matches one of multiple selectors",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			labelSelectors:     []string{"env=prod", "app", "tier=frontend"},
			upgradeStatusLabel: "upgrade-status",
			expected:           true,
			description:        "Node matching one of multiple selectors should return true",
		},
		{
			name: "Node doesn't match any selector",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "api",
					},
				},
			},
			labelSelectors:     []string{"env=prod", "tier=frontend"},
			upgradeStatusLabel: "upgrade-status",
			expected:           false,
			description:        "Node matching none of multiple selectors should return false",
		},
		{
			name: "Empty upgrade status label",
			node: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			labelSelectors:     []string{"app"},
			upgradeStatusLabel: "",
			expected:           true,
			description:        "Node should match when upgrade status label is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			result := nodeMatchesLabelSelectors(tt.node, tt.labelSelectors, tt.upgradeStatusLabel, logger)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestFindNodesWithLabels tests the FindNodesWithLabels function
func TestFindNodesWithLabels(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.Config
		nodes       []v1.Node
		expected    []NodeLabelInfo
		expectError bool
		description string
	}{
		{
			name: "No labels configured",
			cfg: config.Config{
				NodeLabelsToDetect: []string{},
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
			},
			expected:    []NodeLabelInfo{},
			expectError: false,
			description: "Should return empty list when no labels configured",
		},
		{
			name: "Nodes match label criteria",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app=web", "env=prod"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"env": "prod",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"app": "api",
						},
					},
				},
			},
			expected: []NodeLabelInfo{
				{
					Name: "node1",
					Labels: map[string]string{
						"app": "web",
					},
				},
				{
					Name: "node2",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			expectError: false,
			description: "Should return nodes that match any of the label selectors",
		},
		{
			name: "All nodes already parked",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"app":            "web",
							"upgrade-status": "parked",
						},
					},
				},
			},
			expected:    []NodeLabelInfo{},
			expectError: false,
			description: "Should exclude already parked nodes",
		},
		{
			name: "Some nodes already parked",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"app":            "web",
							"upgrade-status": "parked",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
			},
			expected: []NodeLabelInfo{
				{
					Name: "node2",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			expectError: false,
			description: "Should exclude parked nodes but include matching unparked nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewClientset()

			// Add nodes to the fake client
			for _, node := range tt.nodes {
				_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), &node, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			logger := log.NewEntry(log.New())
			result, err := FindNodesWithLabels(context.Background(), fakeClient, tt.cfg, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, len(tt.expected), len(result), tt.description)

				// Check that all expected nodes are in the result
				for _, expectedNode := range tt.expected {
					found := false
					for _, resultNode := range result {
						if resultNode.Name == expectedNode.Name {
							found = true
							assert.Equal(t, expectedNode.Labels, resultNode.Labels)
							break
						}
					}
					assert.True(t, found, "Expected node %s not found in result", expectedNode.Name)
				}
			}
		})
	}
}

// TestParkNodesWithLabels tests the ParkNodesWithLabels function
func TestParkNodesWithLabels(t *testing.T) {
	tests := []struct {
		name          string
		matchingNodes []NodeLabelInfo
		cfg           config.Config
		dryRun        bool
		expectError   bool
		description   string
	}{
		{
			name:          "No matching nodes",
			matchingNodes: []NodeLabelInfo{},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
			},
			dryRun:      false,
			expectError: false,
			description: "Should return nil when no nodes to park",
		},
		{
			name: "Single matching node",
			matchingNodes: []NodeLabelInfo{
				{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
			},
			dryRun:      false,
			expectError: false,
			description: "Should park single matching node",
		},
		{
			name: "Multiple matching nodes",
			matchingNodes: []NodeLabelInfo{
				{
					Name: "node1",
					Labels: map[string]string{
						"app": "web",
					},
				},
				{
					Name: "node2",
					Labels: map[string]string{
						"env": "prod",
					},
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
			},
			dryRun:      false,
			expectError: false,
			description: "Should park multiple matching nodes",
		},
		{
			name: "Dry run mode",
			matchingNodes: []NodeLabelInfo{
				{
					Name: "test-node",
					Labels: map[string]string{
						"app": "web",
					},
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
			},
			dryRun:      true,
			expectError: false,
			description: "Should work in dry run mode",
		},
		{
			name: "MaxParkedNodes limit applied",
			matchingNodes: []NodeLabelInfo{
				{
					Name: "node1",
					Labels: map[string]string{
						"app": "web",
					},
				},
				{
					Name: "node2",
					Labels: map[string]string{
						"env": "prod",
					},
				},
				{
					Name: "node3",
					Labels: map[string]string{
						"tier": "frontend",
					},
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "2",
				UpgradeStatusLabel: "upgrade-status",
			},
			dryRun:      false,
			expectError: false,
			description: "Should respect MaxParkedNodes limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewClientset()

			// Add nodes to the fake client if they exist
			for _, nodeInfo := range tt.matchingNodes {
				node := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:   nodeInfo.Name,
						Labels: nodeInfo.Labels,
					},
				}
				_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			logger := log.NewEntry(log.New())
			err := ParkNodesWithLabels(context.Background(), fakeClient, tt.matchingNodes, tt.cfg, tt.dryRun, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestProcessNodesWithLabels tests the ProcessNodesWithLabels function
func TestProcessNodesWithLabels(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.Config
		nodes       []v1.Node
		expectError bool
		description string
	}{
		{
			name: "No nodes match criteria",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app=web"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"app": "api",
						},
					},
				},
			},
			expectError: false,
			description: "Should return nil when no nodes match criteria",
		},
		{
			name: "Nodes match criteria",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app=web"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node",
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
			},
			expectError: false,
			description: "Should process nodes that match criteria",
		},
		{
			name: "Multiple nodes match criteria",
			cfg: config.Config{
				NodeLabelsToDetect: []string{"app=web", "env=prod"},
				UpgradeStatusLabel: "upgrade-status",
			},
			nodes: []v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"app": "web",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"env": "prod",
						},
					},
				},
			},
			expectError: false,
			description: "Should process multiple nodes that match criteria",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewClientset()

			// Add nodes to the fake client
			for _, node := range tt.nodes {
				_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), &node, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			// Create app context
			appContext := &AppContext{
				K8sClient: fakeClient,
				Config:    tt.cfg,
			}

			logger := log.NewEntry(log.New())
			err := ProcessNodesWithLabels(context.Background(), appContext, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}
