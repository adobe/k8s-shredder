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
	"strconv"
	"testing"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/fake"
)

// TestIsNodeClaimDrifted tests the isNodeClaimDrifted function
func TestIsNodeClaimDrifted(t *testing.T) {
	tests := []struct {
		name        string
		nodeClaim   map[string]interface{}
		expected    bool
		expectError bool
		description string
	}{
		{
			name: "NodeClaim is drifted",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Drifted",
							"status": "True",
						},
					},
				},
			},
			expected:    true,
			expectError: false,
			description: "NodeClaim with Drifted=True condition should return true",
		},
		{
			name: "NodeClaim is not drifted",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Drifted",
							"status": "False",
						},
					},
				},
			},
			expected:    false,
			expectError: false,
			description: "NodeClaim with Drifted=False condition should return false",
		},
		{
			name: "NodeClaim has no Drifted condition",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			},
			expected:    false,
			expectError: false,
			description: "NodeClaim without Drifted condition should return false",
		},
		{
			name: "NodeClaim has no conditions",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{},
			},
			expected:    false,
			expectError: false,
			description: "NodeClaim with no conditions should return false",
		},
		{
			name:        "NodeClaim has no status",
			nodeClaim:   map[string]interface{}{},
			expected:    false,
			expectError: false,
			description: "NodeClaim with no status should return false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			result, err := isNodeClaimDrifted(tt.nodeClaim, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

// TestIsNodeClaimStuckTerminating tests the isNodeClaimStuckTerminating function
func TestIsNodeClaimStuckTerminating(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ttl := 1 * time.Hour

	tests := []struct {
		name           string
		nodeClaim      map[string]interface{}
		expectedStuck  bool
		expectedTSZero bool
		expectError    bool
		description    string
	}{
		{
			name: "NodeClaim with no deletionTimestamp",
			nodeClaim: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "nodeclaim-1",
				},
			},
			expectedStuck:  false,
			expectedTSZero: true,
			expectError:    false,
			description:    "NodeClaim not being deleted should not be stuck",
		},
		{
			name: "NodeClaim deleted recently, within TTL",
			nodeClaim: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "nodeclaim-1",
					"deletionTimestamp": now.Add(-30 * time.Minute).Format(time.RFC3339),
				},
			},
			expectedStuck:  false,
			expectedTSZero: true,
			expectError:    false,
			description:    "NodeClaim deleted less than ttl ago should not be considered stuck yet",
		},
		{
			name: "NodeClaim deleted exactly at TTL boundary",
			nodeClaim: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "nodeclaim-1",
					"deletionTimestamp": now.Add(-ttl).Format(time.RFC3339),
				},
			},
			expectedStuck:  false,
			expectedTSZero: true,
			expectError:    false,
			description:    "NodeClaim deleted exactly ttl ago should not yet be considered stuck (strictly greater than)",
		},
		{
			name: "NodeClaim deleted well past TTL",
			nodeClaim: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "nodeclaim-1",
					"deletionTimestamp": now.Add(-24 * time.Hour).Format(time.RFC3339),
				},
			},
			expectedStuck:  true,
			expectedTSZero: false,
			expectError:    false,
			description:    "NodeClaim deleted well past ttl ago should be considered stuck",
		},
		{
			name: "NodeClaim with malformed deletionTimestamp",
			nodeClaim: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "nodeclaim-1",
					"deletionTimestamp": "not-a-timestamp",
				},
			},
			expectedStuck:  false,
			expectedTSZero: true,
			expectError:    true,
			description:    "Malformed deletionTimestamp should return an error",
		},
		{
			name:           "NodeClaim with no metadata at all",
			nodeClaim:      map[string]interface{}{},
			expectedStuck:  false,
			expectedTSZero: true,
			expectError:    false,
			description:    "NodeClaim with no metadata should not be stuck",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stuck, deletionTimestamp, err := isNodeClaimStuckTerminating(tt.nodeClaim, now, ttl)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
			assert.Equal(t, tt.expectedStuck, stuck, tt.description)
			assert.Equal(t, tt.expectedTSZero, deletionTimestamp.IsZero(), tt.description)
		})
	}
}

// TestGetNodeInfoFromNodeClaim tests the getNodeInfoFromNodeClaim function
func TestGetNodeInfoFromNodeClaim(t *testing.T) {
	tests := []struct {
		name             string
		nodeClaim        map[string]interface{}
		expectedNode     string
		expectedProvider string
		description      string
	}{
		{
			name: "NodeClaim with node info",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"nodeName":   "test-node",
					"providerID": "aws://us-west-2a/i-1234567890abcdef0",
				},
			},
			expectedNode:     "test-node",
			expectedProvider: "aws://us-west-2a/i-1234567890abcdef0",
			description:      "Should extract node name and provider ID",
		},
		{
			name: "NodeClaim with only node name",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"nodeName": "test-node",
				},
			},
			expectedNode:     "test-node",
			expectedProvider: "",
			description:      "Should extract node name when provider ID is missing",
		},
		{
			name: "NodeClaim with only provider ID",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"providerID": "aws://us-west-2a/i-1234567890abcdef0",
				},
			},
			expectedNode:     "",
			expectedProvider: "aws://us-west-2a/i-1234567890abcdef0",
			description:      "Should extract provider ID when node name is missing",
		},
		{
			name: "NodeClaim with no node info",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{},
			},
			expectedNode:     "",
			expectedProvider: "",
			description:      "Should return empty strings when no node info",
		},
		{
			name:             "NodeClaim with no status",
			nodeClaim:        map[string]interface{}{},
			expectedNode:     "",
			expectedProvider: "",
			description:      "Should return empty strings when no status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			nodeName, providerID := getNodeInfoFromNodeClaim(tt.nodeClaim, logger)

			assert.Equal(t, tt.expectedNode, nodeName, tt.description)
			assert.Equal(t, tt.expectedProvider, providerID, tt.description)
		})
	}
}

// TestLabelDriftedNodes tests the LabelDriftedNodes function
func TestLabelDriftedNodes(t *testing.T) {
	tests := []struct {
		name              string
		driftedNodeClaims []KarpenterNodeClaimInfo
		cfg               config.Config
		dryRun            bool
		expectError       bool
		description       string
	}{
		{
			name:              "No drifted node claims",
			driftedNodeClaims: []KarpenterNodeClaimInfo{},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should return nil when no drifted node claims",
		},
		{
			name: "Single drifted node claim",
			driftedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:       "nodeclaim-1",
					Namespace:  "default",
					NodeName:   "test-node",
					ProviderID: "aws://us-west-2a/i-1234567890abcdef0",
					IsDrifted:  true,
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should label single drifted node",
		},
		{
			name: "Node claim without node name",
			driftedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:       "nodeclaim-1",
					Namespace:  "default",
					NodeName:   "",
					ProviderID: "aws://us-west-2a/i-1234567890abcdef0",
					IsDrifted:  true,
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should skip node claim without node name",
		},
		{
			name: "Dry run mode",
			driftedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:       "nodeclaim-1",
					Namespace:  "default",
					NodeName:   "test-node",
					ProviderID: "aws://us-west-2a/i-1234567890abcdef0",
					IsDrifted:  true,
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      true,
			expectError: false,
			description: "Should work in dry run mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewClientset()

			// Add nodes to the fake client if they have node names
			for _, nodeClaim := range tt.driftedNodeClaims {
				if nodeClaim.NodeName != "" {
					node := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: nodeClaim.NodeName,
						},
					}
					_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
					assert.NoError(t, err)
				}
			}

			logger := log.NewEntry(log.New())
			err := LabelDriftedNodes(context.Background(), fakeClient, tt.driftedNodeClaims, tt.cfg, tt.dryRun, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestKarpenterNodeClaimInfo tests the KarpenterNodeClaimInfo struct
func TestKarpenterNodeClaimInfo(t *testing.T) {
	nodeClaimInfo := KarpenterNodeClaimInfo{
		Name:       "test-nodeclaim",
		Namespace:  "default",
		NodeName:   "test-node",
		ProviderID: "aws://us-west-2a/i-1234567890abcdef0",
		IsDrifted:  true,
	}

	assert.Equal(t, "test-nodeclaim", nodeClaimInfo.Name)
	assert.Equal(t, "default", nodeClaimInfo.Namespace)
	assert.Equal(t, "test-node", nodeClaimInfo.NodeName)
	assert.Equal(t, "aws://us-west-2a/i-1234567890abcdef0", nodeClaimInfo.ProviderID)
	assert.True(t, nodeClaimInfo.IsDrifted)
}

// TestIsNodeClaimDisrupted tests the isNodeClaimDisrupted function
func TestIsNodeClaimDisrupted(t *testing.T) {
	tests := []struct {
		name              string
		nodeClaim         map[string]interface{}
		expectedDisrupted bool
		expectedReason    string
		expectError       bool
		description       string
	}{
		{
			name: "NodeClaim has DisruptionReason=Drifted",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "DisruptionReason",
							"status": "True",
							"reason": "Drifted",
						},
					},
				},
			},
			expectedDisrupted: true,
			expectedReason:    "Drifted",
			expectError:       false,
			description:       "NodeClaim with DisruptionReason=True/Drifted condition should return true",
		},
		{
			name: "NodeClaim has DisruptionReason=Empty",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "DisruptionReason",
							"status": "True",
							"reason": "Empty",
						},
					},
				},
			},
			expectedDisrupted: true,
			expectedReason:    "Empty",
			expectError:       false,
			description:       "NodeClaim with DisruptionReason=True/Empty condition should return true",
		},
		{
			name: "NodeClaim has DisruptionReason=Underutilized",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "DisruptionReason",
							"status": "True",
							"reason": "Underutilized",
						},
					},
				},
			},
			expectedDisrupted: true,
			expectedReason:    "Underutilized",
			expectError:       false,
			description:       "NodeClaim with DisruptionReason=True/Underutilized condition should return true",
		},
		{
			name: "NodeClaim is instance terminating",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "InstanceTerminating",
							"status": "True",
						},
					},
				},
			},
			expectedDisrupted: true,
			expectedReason:    "InstanceTerminating",
			expectError:       false,
			description:       "NodeClaim with InstanceTerminating=True condition should return true, falling back to the condition type as the reason",
		},
		{
			name: "NodeClaim has DisruptionReason=False",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "DisruptionReason",
							"status": "False",
							"reason": "Drifted",
						},
					},
				},
			},
			expectedDisrupted: false,
			expectedReason:    "",
			expectError:       false,
			description:       "NodeClaim with DisruptionReason=False condition should return false",
		},
		{
			name: "NodeClaim has no disruption conditions",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{
					"conditions": []interface{}{
						map[string]interface{}{
							"type":   "Ready",
							"status": "True",
						},
					},
				},
			},
			expectedDisrupted: false,
			expectedReason:    "",
			expectError:       false,
			description:       "NodeClaim without disruption conditions should return false",
		},
		{
			name: "NodeClaim has no conditions",
			nodeClaim: map[string]interface{}{
				"status": map[string]interface{}{},
			},
			expectedDisrupted: false,
			expectedReason:    "",
			expectError:       false,
			description:       "NodeClaim with no conditions should return false",
		},
		{
			name:              "NodeClaim has no status",
			nodeClaim:         map[string]interface{}{},
			expectedDisrupted: false,
			expectedReason:    "",
			expectError:       false,
			description:       "NodeClaim with no status should return false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			disrupted, reason, err := isNodeClaimDisrupted(tt.nodeClaim, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expectedDisrupted, disrupted, tt.description)
				assert.Equal(t, tt.expectedReason, reason, tt.description)
			}
		})
	}
}

// TestLabelDisruptedNodes tests the LabelDisruptedNodes function
func TestLabelDisruptedNodes(t *testing.T) {
	tests := []struct {
		name                string
		disruptedNodeClaims []KarpenterNodeClaimInfo
		cfg                 config.Config
		dryRun              bool
		expectError         bool
		description         string
	}{
		{
			name:                "No disrupted node claims",
			disruptedNodeClaims: []KarpenterNodeClaimInfo{},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should return nil when no disrupted node claims",
		},
		{
			name: "Single disrupted node claim",
			disruptedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:             "nodeclaim-1",
					Namespace:        "default",
					NodeName:         "test-node",
					ProviderID:       "aws://us-west-2a/i-1234567890abcdef0",
					IsDisrupted:      true,
					DisruptionReason: "Drifted",
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should label single disrupted node",
		},
		{
			name: "Node claim without node name",
			disruptedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:             "nodeclaim-1",
					Namespace:        "default",
					NodeName:         "",
					ProviderID:       "aws://us-west-2a/i-1234567890abcdef0",
					IsDisrupted:      true,
					DisruptionReason: "Empty",
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      false,
			expectError: false,
			description: "Should skip node claim without node name",
		},
		{
			name: "Dry run mode",
			disruptedNodeClaims: []KarpenterNodeClaimInfo{
				{
					Name:             "nodeclaim-1",
					Namespace:        "default",
					NodeName:         "test-node",
					ProviderID:       "aws://us-west-2a/i-1234567890abcdef0",
					IsDisrupted:      true,
					DisruptionReason: "Underutilized",
				},
			},
			cfg: config.Config{
				MaxParkedNodes:     "5",
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
			},
			dryRun:      true,
			expectError: false,
			description: "Should work in dry run mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes client
			fakeClient := fake.NewClientset()

			// Add nodes to the fake client if they have node names
			for _, nodeClaim := range tt.disruptedNodeClaims {
				if nodeClaim.NodeName != "" {
					node := &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: nodeClaim.NodeName,
						},
					}
					_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
					assert.NoError(t, err)
				}
			}

			logger := log.NewEntry(log.New())
			err := LabelDisruptedNodes(context.Background(), fakeClient, tt.disruptedNodeClaims, tt.cfg, tt.dryRun, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestLabelStuckTerminatingNodes tests the LabelStuckTerminatingNodes function, in particular
// that the resulting ExpiresOnLabel is computed from the NodeClaim's DeletionTimestamp rather
// than from time.Now() - a node already stuck past ParkedNodeTTL should come out with an
// already-expired label instead of a fresh one.
func TestLabelStuckTerminatingNodes(t *testing.T) {
	cfg := config.Config{
		MaxParkedNodes:     "5",
		UpgradeStatusLabel: "upgrade-status",
		ExpiresOnLabel:     "expires-on",
		ParkedByLabel:      "parked-by",
		ParkedByValue:      "k8s-shredder",
		ParkingReasonLabel: "parked-reason",
		ParkedNodeTaint:    "upgrade-status=parked:NoSchedule",
		ParkedNodeTTL:      1 * time.Hour,
	}

	t.Run("No stuck node claims", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		logger := log.NewEntry(log.New())
		err := LabelStuckTerminatingNodes(context.Background(), fakeClient, []KarpenterNodeClaimInfo{}, cfg, false, logger)
		assert.NoError(t, err)
	})

	t.Run("Node claim without node name is skipped", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		logger := log.NewEntry(log.New())
		err := LabelStuckTerminatingNodes(context.Background(), fakeClient, []KarpenterNodeClaimInfo{
			{
				Name:               "nodeclaim-1",
				NodeName:           "",
				IsStuckTerminating: true,
				DeletionTimestamp:  time.Now().Add(-24 * time.Hour),
			},
		}, cfg, false, logger)
		assert.NoError(t, err)
	})

	t.Run("Stuck node claim expiration is backdated to deletionTimestamp", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
			},
		}
		_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NoError(t, err)

		deletionTimestamp := time.Now().Add(-24 * time.Hour)
		logger := log.NewEntry(log.New())
		err = LabelStuckTerminatingNodes(context.Background(), fakeClient, []KarpenterNodeClaimInfo{
			{
				Name:               "nodeclaim-1",
				NodeName:           "test-node",
				IsStuckTerminating: true,
				DeletionTimestamp:  deletionTimestamp,
			},
		}, cfg, false, logger)
		assert.NoError(t, err)

		updatedNode, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "test-node", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, "parked", updatedNode.Labels[cfg.UpgradeStatusLabel])

		expiresOnStr := updatedNode.Labels[cfg.ExpiresOnLabel]
		assert.NotEmpty(t, expiresOnStr)

		expiresOnUnix, err := strconv.ParseInt(expiresOnStr, 10, 64)
		assert.NoError(t, err)

		expectedExpiry := deletionTimestamp.Add(cfg.ParkedNodeTTL)
		// The expiry should be anchored to deletionTimestamp+TTL, i.e. already in the past,
		// not to time.Now()+TTL which would still be in the future.
		assert.WithinDuration(t, expectedExpiry, time.Unix(expiresOnUnix, 0), 2*time.Second)
		assert.True(t, time.Unix(expiresOnUnix, 0).Before(time.Now()), "expiry backdated from deletionTimestamp should already be in the past")
	})
}

// TestProcessDriftedKarpenterNodes tests the ProcessDriftedKarpenterNodes function
func TestProcessDriftedKarpenterNodes(t *testing.T) {
	tests := []struct {
		name        string
		appContext  *AppContext
		expectError bool
		description string
	}{
		{
			name: "No drifted node claims found",
			appContext: &AppContext{
				Config: config.Config{
					UpgradeStatusLabel: "upgrade-status",
					ParkingReasonLabel: "parked-reason",
				},
				K8sClient:        fake.NewClientset(),
				DynamicK8SClient: &fakeDynamicClient{},
				dryRun:           false,
			},
			expectError: false,
			description: "Should return nil when no drifted node claims are found",
		},
		{
			name: "Drifted node claims found and processed successfully",
			appContext: &AppContext{
				Config: config.Config{
					UpgradeStatusLabel: "upgrade-status",
					MaxParkedNodes:     "5",
					ParkingReasonLabel: "parked-reason",
				},
				K8sClient:        fake.NewClientset(),
				DynamicK8SClient: &fakeDynamicClientWithDriftedClaims{},
				dryRun:           true,
			},
			expectError: false,
			description: "Should process drifted node claims successfully in dry-run mode",
		},
		{
			name: "Error finding drifted node claims",
			appContext: &AppContext{
				Config: config.Config{
					UpgradeStatusLabel: "upgrade-status",
					ParkingReasonLabel: "parked-reason",
				},
				K8sClient:        fake.NewClientset(),
				DynamicK8SClient: &fakeDynamicClientWithError{},
				dryRun:           false,
			},
			expectError: true,
			description: "Should return error when finding drifted node claims fails",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.NewEntry(log.New())
			err := ProcessDriftedKarpenterNodes(context.Background(), tt.appContext, logger)

			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestProcessStuckTerminatingNodeClaims tests the ProcessStuckTerminatingNodeClaims function
func TestProcessStuckTerminatingNodeClaims(t *testing.T) {
	t.Run("No stuck terminating node claims found", func(t *testing.T) {
		appContext := &AppContext{
			Config: config.Config{
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
				ParkedNodeTTL:      1 * time.Hour,
			},
			K8sClient:        fake.NewClientset(),
			DynamicK8SClient: &fakeDynamicClient{},
			dryRun:           false,
		}
		logger := log.NewEntry(log.New())
		err := ProcessStuckTerminatingNodeClaims(context.Background(), appContext, logger)
		assert.NoError(t, err)
	})

	t.Run("Stuck terminating node claim found and parked with backdated expiry", func(t *testing.T) {
		fakeClient := fake.NewClientset()
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "stuck-terminating-node-1",
			},
		}
		_, err := fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		assert.NoError(t, err)

		appContext := &AppContext{
			Config: config.Config{
				UpgradeStatusLabel: "upgrade-status",
				ExpiresOnLabel:     "expires-on",
				ParkedByLabel:      "parked-by",
				ParkedByValue:      "k8s-shredder",
				ParkingReasonLabel: "parked-reason",
				ParkedNodeTaint:    "upgrade-status=parked:NoSchedule",
				MaxParkedNodes:     "5",
				ParkedNodeTTL:      1 * time.Hour,
			},
			K8sClient:        fakeClient,
			DynamicK8SClient: &fakeDynamicClientWithStuckTerminatingClaims{},
			dryRun:           false,
		}
		logger := log.NewEntry(log.New())
		err = ProcessStuckTerminatingNodeClaims(context.Background(), appContext, logger)
		assert.NoError(t, err)

		updatedNode, err := fakeClient.CoreV1().Nodes().Get(context.Background(), "stuck-terminating-node-1", metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, "parked", updatedNode.Labels["upgrade-status"])

		expiresOnUnix, err := strconv.ParseInt(updatedNode.Labels["expires-on"], 10, 64)
		assert.NoError(t, err)
		assert.True(t, time.Unix(expiresOnUnix, 0).Before(time.Now()), "a NodeClaim already stuck past ParkedNodeTTL should be parked with an already-expired expiry")
	})

	t.Run("Error finding stuck terminating node claims", func(t *testing.T) {
		appContext := &AppContext{
			Config: config.Config{
				UpgradeStatusLabel: "upgrade-status",
				ParkingReasonLabel: "parked-reason",
				ParkedNodeTTL:      1 * time.Hour,
			},
			K8sClient:        fake.NewClientset(),
			DynamicK8SClient: &fakeDynamicClientWithError{},
			dryRun:           false,
		}
		logger := log.NewEntry(log.New())
		err := ProcessStuckTerminatingNodeClaims(context.Background(), appContext, logger)
		assert.Error(t, err)
	})
}

// fakeDynamicClient implements dynamic.Interface for testing
type fakeDynamicClient struct{}

func (f *fakeDynamicClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &fakeNamespaceableResourceInterface{}
}

// fakeDynamicClientWithDriftedClaims provides drifted NodeClaims for testing
type fakeDynamicClientWithDriftedClaims struct{}

func (f *fakeDynamicClientWithDriftedClaims) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &fakeNamespaceableResourceInterfaceWithDriftedClaims{}
}

// fakeDynamicClientWithStuckTerminatingClaims provides a NodeClaim stuck terminating well past
// any reasonable ParkedNodeTTL
type fakeDynamicClientWithStuckTerminatingClaims struct{}

func (f *fakeDynamicClientWithStuckTerminatingClaims) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims{}
}

// fakeDynamicClientWithError returns errors for testing
type fakeDynamicClientWithError struct{}

func (f *fakeDynamicClientWithError) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &fakeNamespaceableResourceInterfaceWithError{}
}

// fakeNamespaceableResourceInterface implements dynamic.NamespaceableResourceInterface
type fakeNamespaceableResourceInterface struct{}

func (f *fakeNamespaceableResourceInterface) Namespace(string) dynamic.ResourceInterface {
	return &fakeResourceInterface{}
}

func (f *fakeNamespaceableResourceInterface) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{},
	}, nil
}

func (f *fakeNamespaceableResourceInterface) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeNamespaceableResourceInterface) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeNamespaceableResourceInterface) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterface) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeNamespaceableResourceInterfaceWithDriftedClaims provides drifted NodeClaims
type fakeNamespaceableResourceInterfaceWithDriftedClaims struct{}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Namespace(string) dynamic.ResourceInterface {
	return &fakeResourceInterfaceWithDriftedClaims{}
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	driftedNodeClaim := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "drifted-nodeclaim-1",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"nodeName":   "test-node-1",
				"providerID": "aws://us-west-2a/i-1234567890abcdef0",
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Drifted",
						"status": "True",
					},
				},
			},
		},
	}

	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{driftedNodeClaim},
	}, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithDriftedClaims) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims provides a NodeClaim stuck
// terminating well past any reasonable ParkedNodeTTL
type fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims struct{}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Namespace(string) dynamic.ResourceInterface {
	return &fakeResourceInterfaceWithStuckTerminatingClaims{}
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	stuckNodeClaim := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":              "stuck-terminating-nodeclaim-1",
				"namespace":         "default",
				"deletionTimestamp": "2020-01-01T00:00:00Z",
			},
			"status": map[string]interface{}{
				"nodeName":   "stuck-terminating-node-1",
				"providerID": "aws://us-west-2a/i-0987654321fedcba0",
			},
		},
	}

	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{stuckNodeClaim},
	}, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeNamespaceableResourceInterfaceWithStuckTerminatingClaims) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeNamespaceableResourceInterfaceWithError returns errors
type fakeNamespaceableResourceInterfaceWithError struct{}

func (f *fakeNamespaceableResourceInterfaceWithError) Namespace(string) dynamic.ResourceInterface {
	return &fakeResourceInterfaceWithError{}
}

func (f *fakeNamespaceableResourceInterfaceWithError) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return nil, errors.New("failed to list NodeClaims")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("create error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("update error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("update status error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return errors.New("delete error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return errors.New("delete collection error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("get error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("watch error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("patch error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("apply error")
}

func (f *fakeNamespaceableResourceInterfaceWithError) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("apply status error")
}

// fakeResourceInterface implements dynamic.ResourceInterface
type fakeResourceInterface struct{}

func (f *fakeResourceInterface) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeResourceInterface) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeResourceInterface) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{},
	}, nil
}

func (f *fakeResourceInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeResourceInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterface) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeResourceInterfaceWithDriftedClaims provides drifted NodeClaims
type fakeResourceInterfaceWithDriftedClaims struct{}

func (f *fakeResourceInterfaceWithDriftedClaims) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	driftedNodeClaim := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      "drifted-nodeclaim-1",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"nodeName":   "test-node-1",
				"providerID": "aws://us-west-2a/i-1234567890abcdef0",
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Drifted",
						"status": "True",
					},
				},
			},
		},
	}

	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{driftedNodeClaim},
	}, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithDriftedClaims) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeResourceInterfaceWithStuckTerminatingClaims provides a NodeClaim stuck terminating well
// past any reasonable ParkedNodeTTL
type fakeResourceInterfaceWithStuckTerminatingClaims struct{}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	stuckNodeClaim := unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":              "stuck-terminating-nodeclaim-1",
				"namespace":         "default",
				"deletionTimestamp": "2020-01-01T00:00:00Z",
			},
			"status": map[string]interface{}{
				"nodeName":   "stuck-terminating-node-1",
				"providerID": "aws://us-west-2a/i-0987654321fedcba0",
			},
		},
	}

	return &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{stuckNodeClaim},
	}, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (f *fakeResourceInterfaceWithStuckTerminatingClaims) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// fakeResourceInterfaceWithError returns errors
type fakeResourceInterfaceWithError struct{}

func (f *fakeResourceInterfaceWithError) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("create error")
}

func (f *fakeResourceInterfaceWithError) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("update error")
}

func (f *fakeResourceInterfaceWithError) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("update status error")
}

func (f *fakeResourceInterfaceWithError) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	return errors.New("delete error")
}

func (f *fakeResourceInterfaceWithError) DeleteCollection(ctx context.Context, options metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return errors.New("delete collection error")
}

func (f *fakeResourceInterfaceWithError) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("get error")
}

func (f *fakeResourceInterfaceWithError) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return nil, errors.New("list error")
}

func (f *fakeResourceInterfaceWithError) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, errors.New("watch error")
}

func (f *fakeResourceInterfaceWithError) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, options metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("patch error")
}

func (f *fakeResourceInterfaceWithError) Apply(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, errors.New("apply error")
}

func (f *fakeResourceInterfaceWithError) ApplyStatus(ctx context.Context, name string, obj *unstructured.Unstructured, options metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, errors.New("apply status error")
}
