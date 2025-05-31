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
	"context"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	// Karpenter API constants
	KarpenterAPIGroup   = "karpenter.sh"
	KarpenterAPIVersion = "v1"
	NodeClaimResource   = "nodeclaims"

	// Karpenter condition types
	KarpenterDriftedCondition = "Drifted"
	KarpenterTrueStatus       = "True"
)

// KarpenterNodeClaimInfo holds information about a Karpenter NodeClaim
type KarpenterNodeClaimInfo struct {
	Name       string
	Namespace  string
	NodeName   string
	ProviderID string
	IsDrifted  bool
}

// FindDriftedKarpenterNodeClaims scans the kubernetes cluster for Karpenter NodeClaims that are marked as drifted
// and excludes nodes that are already labeled as parked
func FindDriftedKarpenterNodeClaims(ctx context.Context, dynamicClient dynamic.Interface, k8sClient kubernetes.Interface, cfg config.Config, logger *log.Entry) ([]KarpenterNodeClaimInfo, error) {
	logger = logger.WithField("function", "FindDriftedKarpenterNodeClaims")

	// Create a GVR for Karpenter NodeClaims
	gvr := schema.GroupVersionResource{
		Group:    KarpenterAPIGroup,
		Version:  KarpenterAPIVersion,
		Resource: NodeClaimResource,
	}

	logger.Info("Listing Karpenter NodeClaims")

	// List all NodeClaims
	nodeClaimList, err := dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to list Karpenter NodeClaims")
		return nil, errors.Wrap(err, "failed to list Karpenter NodeClaims")
	}

	logger.WithField("totalNodeClaims", len(nodeClaimList.Items)).Debug("Retrieved NodeClaims list")

	var driftedNodeClaims []KarpenterNodeClaimInfo

	for _, item := range nodeClaimList.Items {
		nodeClaim := item.Object

		// Extract NodeClaim name and namespace
		name, _, err := unstructured.NestedString(nodeClaim, "metadata", "name")
		if err != nil || name == "" {
			logger.WithField("nodeclaim", item.GetName()).Warn("Failed to get NodeClaim name")
			continue
		}

		namespace, _, err := unstructured.NestedString(nodeClaim, "metadata", "namespace")
		if err != nil {
			namespace = "default" // NodeClaims might be cluster-scoped
		}

		nodeClaimLogger := logger.WithFields(log.Fields{
			"nodeclaim": name,
			"namespace": namespace,
		})

		// Check if the NodeClaim is drifted by examining its conditions
		isDrifted, err := isNodeClaimDrifted(nodeClaim, nodeClaimLogger)
		if err != nil {
			nodeClaimLogger.WithError(err).Warn("Failed to check drift status")
			continue
		}

		if isDrifted {
			nodeClaimLogger.Debug("NodeClaim is marked as drifted")

			// Get the associated node information
			nodeName, providerID := getNodeInfoFromNodeClaim(nodeClaim, nodeClaimLogger)

			// Skip if no node is associated
			if nodeName == "" {
				nodeClaimLogger.Debug("NodeClaim has no associated node, skipping")
				continue
			}

			nodeClaimLogger = nodeClaimLogger.WithField("nodeName", nodeName)

			// Check if the node is already labeled as parked
			isAlreadyParked, err := isNodeAlreadyParked(ctx, k8sClient, nodeName, cfg.UpgradeStatusLabel, nodeClaimLogger)
			if err != nil {
				nodeClaimLogger.WithError(err).Warn("Failed to check if node is already parked, skipping")
				continue
			}

			if isAlreadyParked {
				nodeClaimLogger.Debug("Node is already labeled as parked, skipping")
				continue
			}

			nodeClaimLogger.Info("Found drifted NodeClaim with unlabeled node")

			driftedNodeClaims = append(driftedNodeClaims, KarpenterNodeClaimInfo{
				Name:       name,
				Namespace:  namespace,
				NodeName:   nodeName,
				ProviderID: providerID,
				IsDrifted:  true,
			})
		} else {
			nodeClaimLogger.Debug("NodeClaim is not drifted")
		}
	}

	logger.WithField("driftedCount", len(driftedNodeClaims)).Info("Found drifted Karpenter NodeClaims")

	return driftedNodeClaims, nil
}

// isNodeClaimDrifted checks if a NodeClaim has the "Drifted" condition set to "True"
func isNodeClaimDrifted(nodeClaim map[string]interface{}, logger *log.Entry) (bool, error) {
	logger.Debug("Checking NodeClaim drift status")

	conditions, found, err := unstructured.NestedSlice(nodeClaim, "status", "conditions")
	if err != nil {
		logger.WithError(err).Error("Failed to get conditions from NodeClaim")
		return false, errors.Wrap(err, "failed to get conditions from NodeClaim")
	}

	if !found {
		logger.Debug("No conditions found on NodeClaim, assuming not drifted")
		return false, nil // No conditions means not drifted
	}

	logger.WithField("conditionsCount", len(conditions)).Debug("Found conditions on NodeClaim")

	for _, conditionInterface := range conditions {
		condition, ok := conditionInterface.(map[string]interface{})
		if !ok {
			continue
		}

		conditionType, _, err := unstructured.NestedString(condition, "type")
		if err != nil {
			continue
		}

		if conditionType == KarpenterDriftedCondition {
			status, _, err := unstructured.NestedString(condition, "status")
			if err != nil {
				continue
			}

			isDrifted := status == KarpenterTrueStatus
			logger.WithFields(log.Fields{
				"conditionType":   conditionType,
				"conditionStatus": status,
				"isDrifted":       isDrifted,
			}).Info("Found Drifted condition on NodeClaim")

			return isDrifted, nil
		}
	}

	logger.Debug("No Drifted condition found on NodeClaim, assuming not drifted")
	return false, nil
}

// getNodeInfoFromNodeClaim extracts node name and provider ID from a NodeClaim
func getNodeInfoFromNodeClaim(nodeClaim map[string]interface{}, logger *log.Entry) (string, string) {
	logger.Debug("Extracting node information from NodeClaim")

	nodeName, _, _ := unstructured.NestedString(nodeClaim, "status", "nodeName")
	providerID, _, _ := unstructured.NestedString(nodeClaim, "status", "providerID")

	logger.WithFields(log.Fields{
		"nodeName":   nodeName,
		"providerID": providerID,
	}).Debug("Extracted node information from NodeClaim")

	return nodeName, providerID
}

// LabelDriftedNodes labels nodes associated with drifted NodeClaims with the configured labels
func LabelDriftedNodes(ctx context.Context, k8sClient kubernetes.Interface, driftedNodeClaims []KarpenterNodeClaimInfo, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithField("function", "LabelDriftedNodes")

	logger.WithField("nodeClaimsCount", len(driftedNodeClaims)).Info("Starting to label drifted nodes")

	// Convert KarpenterNodeClaimInfo to NodeInfo for the common parking function
	var nodesToPark []NodeInfo
	for _, nodeClaimInfo := range driftedNodeClaims {
		if nodeClaimInfo.NodeName == "" {
			logger.WithField("nodeclaim", nodeClaimInfo.Name).Warn("NodeClaim has no associated node, skipping")
			continue
		}

		logger.WithFields(log.Fields{
			"nodeclaim": nodeClaimInfo.Name,
			"nodeName":  nodeClaimInfo.NodeName,
		}).Info("Adding node to parking list")

		nodesToPark = append(nodesToPark, NodeInfo{
			Name:   nodeClaimInfo.NodeName,
			Labels: nil, // We don't need to copy the labels for parking
		})
	}

	logger.WithField("nodesToPark", len(nodesToPark)).Info("Converted NodeClaims to parking list")

	// Use the common parking function
	return ParkNodes(ctx, k8sClient, nodesToPark, cfg, dryRun, "karpenter-drift", logger)
}

// ProcessDriftedKarpenterNodes is the main function that combines finding drifted node claims and labeling their nodes
func ProcessDriftedKarpenterNodes(ctx context.Context, appContext *AppContext, logger *log.Entry) error {
	logger = logger.WithField("function", "ProcessDriftedKarpenterNodes")

	logger.Info("Starting Karpenter drift detection and node labeling process")

	// Find drifted Karpenter NodeClaims
	driftedNodeClaims, err := FindDriftedKarpenterNodeClaims(ctx, appContext.DynamicK8SClient, appContext.K8sClient, appContext.Config, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to find drifted Karpenter NodeClaims")
		return errors.Wrap(err, "failed to find drifted Karpenter NodeClaims")
	}

	if len(driftedNodeClaims) == 0 {
		logger.Info("No drifted Karpenter NodeClaims found")
		return nil
	}

	// Label the nodes associated with drifted NodeClaims
	err = LabelDriftedNodes(ctx, appContext.K8sClient, driftedNodeClaims, appContext.Config, appContext.IsDryRun(), logger)
	if err != nil {
		logger.WithError(err).Error("Failed to label drifted nodes")
		return errors.Wrap(err, "failed to label drifted nodes")
	}

	logger.WithField("processedNodes", len(driftedNodeClaims)).Info("Completed Karpenter drift detection and node labeling process")

	return nil
}
