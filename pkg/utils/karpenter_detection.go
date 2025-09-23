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
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/adobe/k8s-shredder/pkg/metrics"
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
	KarpenterDriftedCondition       = "Drifted"
	KarpenterDisruptingCondition    = "Disrupting"
	KarpenterTerminatingCondition   = "Terminating"
	KarpenterEmptyCondition         = "Empty"
	KarpenterUnderutilizedCondition = "Underutilized"
	KarpenterTrueStatus             = "True"
)

// KarpenterNodeClaimInfo holds information about a Karpenter NodeClaim
type KarpenterNodeClaimInfo struct {
	Name             string
	Namespace        string
	NodeName         string
	ProviderID       string
	IsDrifted        bool
	IsDisrupted      bool
	DisruptionReason string
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

// FindDisruptedKarpenterNodeClaims scans the kubernetes cluster for Karpenter NodeClaims that are marked as disrupted
// and excludes nodes that are already labeled as parked
func FindDisruptedKarpenterNodeClaims(ctx context.Context, dynamicClient dynamic.Interface, k8sClient kubernetes.Interface, cfg config.Config, logger *log.Entry) ([]KarpenterNodeClaimInfo, error) {
	logger = logger.WithField("function", "FindDisruptedKarpenterNodeClaims")

	// Create a GVR for Karpenter NodeClaims
	gvr := schema.GroupVersionResource{
		Group:    KarpenterAPIGroup,
		Version:  KarpenterAPIVersion,
		Resource: NodeClaimResource,
	}

	logger.Info("Listing Karpenter NodeClaims for disruption detection")

	// List all NodeClaims
	nodeClaimList, err := dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to list Karpenter NodeClaims")
		return nil, errors.Wrap(err, "failed to list Karpenter NodeClaims")
	}

	logger.WithField("totalNodeClaims", len(nodeClaimList.Items)).Debug("Retrieved NodeClaims list")

	var disruptedNodeClaims []KarpenterNodeClaimInfo

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

		// Check if the NodeClaim is disrupted by examining its conditions
		isDisrupted, disruptionReason, err := isNodeClaimDisrupted(nodeClaim, nodeClaimLogger)
		if err != nil {
			nodeClaimLogger.WithError(err).Warn("Failed to check disruption status")
			continue
		}

		if isDisrupted {
			nodeClaimLogger.WithField("disruptionReason", disruptionReason).Debug("NodeClaim is marked as disrupted")

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

			nodeClaimLogger.WithField("disruptionReason", disruptionReason).Info("Found disrupted NodeClaim with unlabeled node")

			disruptedNodeClaims = append(disruptedNodeClaims, KarpenterNodeClaimInfo{
				Name:             name,
				Namespace:        namespace,
				NodeName:         nodeName,
				ProviderID:       providerID,
				IsDisrupted:      true,
				DisruptionReason: disruptionReason,
			})
		} else {
			nodeClaimLogger.Debug("NodeClaim is not disrupted")
		}
	}

	logger.WithField("disruptedCount", len(disruptedNodeClaims)).Info("Found disrupted Karpenter NodeClaims")

	return disruptedNodeClaims, nil
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

// isNodeClaimDisrupted checks if a NodeClaim has any disruption-related conditions set to "True"
// Returns true if disrupted, the disruption reason, and any error
func isNodeClaimDisrupted(nodeClaim map[string]interface{}, logger *log.Entry) (bool, string, error) {
	logger.Debug("Checking NodeClaim disruption status")

	conditions, found, err := unstructured.NestedSlice(nodeClaim, "status", "conditions")
	if err != nil {
		logger.WithError(err).Error("Failed to get conditions from NodeClaim")
		return false, "", errors.Wrap(err, "failed to get conditions from NodeClaim")
	}

	if !found {
		logger.Debug("No conditions found on NodeClaim, assuming not disrupted")
		return false, "", nil // No conditions means not disrupted
	}

	logger.WithField("conditionsCount", len(conditions)).Debug("Found conditions on NodeClaim")

	// List of disruption conditions to check for
	disruptionConditions := []string{
		KarpenterDisruptingCondition,
		KarpenterTerminatingCondition,
		KarpenterEmptyCondition,
		KarpenterUnderutilizedCondition,
	}

	for _, conditionInterface := range conditions {
		condition, ok := conditionInterface.(map[string]interface{})
		if !ok {
			continue
		}

		conditionType, _, err := unstructured.NestedString(condition, "type")
		if err != nil {
			continue
		}

		// Check if this is a disruption condition
		for _, disruptionCondition := range disruptionConditions {
			if conditionType == disruptionCondition {
				status, _, err := unstructured.NestedString(condition, "status")
				if err != nil {
					continue
				}

				isDisrupted := status == KarpenterTrueStatus
				if isDisrupted {
					logger.WithFields(log.Fields{
						"conditionType":   conditionType,
						"conditionStatus": status,
						"isDisrupted":     isDisrupted,
					}).Info("Found disruption condition on NodeClaim")

					return true, conditionType, nil
				}
			}
		}
	}

	logger.Debug("No disruption conditions found on NodeClaim, assuming not disrupted")
	return false, "", nil
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

	// Apply MaxParkedNodes limit if configured
	limitedNodes, err := LimitNodesToPark(ctx, k8sClient, nodesToPark, cfg.MaxParkedNodes, cfg.UpgradeStatusLabel, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to apply MaxParkedNodes limit")
		return errors.Wrap(err, "failed to apply MaxParkedNodes limit")
	}

	if len(limitedNodes) == 0 {
		logger.Info("No nodes to park after applying MaxParkedNodes limit")
		return nil
	}

	// Use the common parking function
	return ParkNodes(ctx, k8sClient, limitedNodes, cfg, dryRun, "karpenter-drift", logger)
}

// ProcessDriftedKarpenterNodes is the main function that combines finding drifted node claims and labeling their nodes
func ProcessDriftedKarpenterNodes(ctx context.Context, appContext *AppContext, logger *log.Entry) error {
	logger = logger.WithField("function", "ProcessDriftedKarpenterNodes")

	logger.Info("Starting Karpenter drift detection and node labeling process")

	// Start timing the processing duration
	startTime := time.Now()

	// Find drifted Karpenter NodeClaims
	driftedNodeClaims, err := FindDriftedKarpenterNodeClaims(ctx, appContext.DynamicK8SClient, appContext.K8sClient, appContext.Config, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to find drifted Karpenter NodeClaims")
		return errors.Wrap(err, "failed to find drifted Karpenter NodeClaims")
	}

	// Increment the drifted nodes counter
	metrics.ShredderKarpenterDriftedNodesTotal.Add(float64(len(driftedNodeClaims)))

	if len(driftedNodeClaims) == 0 {
		logger.Info("No drifted Karpenter NodeClaims found")
		return nil
	}

	// Label the nodes associated with drifted NodeClaims
	err = LabelDriftedNodes(ctx, appContext.K8sClient, driftedNodeClaims, appContext.Config, appContext.IsDryRun(), logger)
	if err != nil {
		logger.WithError(err).Error("Failed to label drifted nodes")
		metrics.ShredderKarpenterNodesParkingFailedTotal.Add(float64(len(driftedNodeClaims)))
		metrics.ShredderNodesParkingFailedTotal.Add(float64(len(driftedNodeClaims)))
		return errors.Wrap(err, "failed to label drifted nodes")
	}

	// Increment the successfully parked nodes counter
	metrics.ShredderKarpenterNodesParkedTotal.Add(float64(len(driftedNodeClaims)))
	metrics.ShredderNodesParkedTotal.Add(float64(len(driftedNodeClaims)))

	// Record the processing duration
	metrics.ShredderKarpenterProcessingDurationSeconds.Observe(time.Since(startTime).Seconds())
	metrics.ShredderProcessingDurationSeconds.Observe(time.Since(startTime).Seconds())

	logger.WithField("processedNodes", len(driftedNodeClaims)).Info("Completed Karpenter drift detection and node labeling process")

	return nil
}

// LabelDisruptedNodes labels nodes associated with disrupted NodeClaims with the configured labels
func LabelDisruptedNodes(ctx context.Context, k8sClient kubernetes.Interface, disruptedNodeClaims []KarpenterNodeClaimInfo, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithField("function", "LabelDisruptedNodes")

	if len(disruptedNodeClaims) == 0 {
		logger.Debug("No disrupted nodes to label")
		return nil
	}

	logger.WithField("disruptedNodesCount", len(disruptedNodeClaims)).Info("Starting to label disrupted nodes")

	// Convert KarpenterNodeClaimInfo to NodeInfo for the ParkNodes function
	var nodesToPark []NodeInfo
	for _, nodeClaim := range disruptedNodeClaims {
		nodesToPark = append(nodesToPark, NodeInfo{
			Name: nodeClaim.NodeName,
			Labels: map[string]string{
				"karpenter.sh/disruption-reason": nodeClaim.DisruptionReason,
			},
		})
	}

	// Use the unified ParkNodes function to label, cordon, and taint the nodes
	err := ParkNodes(ctx, k8sClient, nodesToPark, cfg, dryRun, "karpenter-disruption", logger)
	if err != nil {
		logger.WithError(err).Error("Failed to park disrupted nodes")
		return errors.Wrap(err, "failed to park disrupted nodes")
	}

	logger.WithField("processedNodes", len(disruptedNodeClaims)).Info("Completed labeling disrupted nodes")
	return nil
}

// ProcessDisruptedKarpenterNodes is the main function that combines finding disrupted node claims and labeling their nodes
func ProcessDisruptedKarpenterNodes(ctx context.Context, appContext *AppContext, logger *log.Entry) error {
	logger = logger.WithField("function", "ProcessDisruptedKarpenterNodes")

	logger.Info("Starting Karpenter disruption detection and node labeling process")

	// Start timing the processing duration
	startTime := time.Now()

	// Find disrupted Karpenter NodeClaims
	disruptedNodeClaims, err := FindDisruptedKarpenterNodeClaims(ctx, appContext.DynamicK8SClient, appContext.K8sClient, appContext.Config, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to find disrupted Karpenter NodeClaims")
		return errors.Wrap(err, "failed to find disrupted Karpenter NodeClaims")
	}

	// Increment the disrupted nodes counter
	metrics.ShredderKarpenterDisruptedNodesTotal.Add(float64(len(disruptedNodeClaims)))

	if len(disruptedNodeClaims) == 0 {
		logger.Info("No disrupted Karpenter NodeClaims found")
		return nil
	}

	// Label the nodes associated with disrupted NodeClaims
	err = LabelDisruptedNodes(ctx, appContext.K8sClient, disruptedNodeClaims, appContext.Config, appContext.IsDryRun(), logger)
	if err != nil {
		logger.WithError(err).Error("Failed to label disrupted nodes")
		metrics.ShredderKarpenterNodesParkingFailedTotal.Add(float64(len(disruptedNodeClaims)))
		metrics.ShredderNodesParkingFailedTotal.Add(float64(len(disruptedNodeClaims)))
		return errors.Wrap(err, "failed to label disrupted nodes")
	}

	// Increment the successfully parked nodes counter
	metrics.ShredderKarpenterNodesParkedTotal.Add(float64(len(disruptedNodeClaims)))
	metrics.ShredderNodesParkedTotal.Add(float64(len(disruptedNodeClaims)))

	// Record the processing duration
	metrics.ShredderKarpenterProcessingDurationSeconds.Observe(time.Since(startTime).Seconds())
	metrics.ShredderProcessingDurationSeconds.Observe(time.Since(startTime).Seconds())

	logger.WithField("processedNodes", len(disruptedNodeClaims)).Info("Completed Karpenter disruption detection and node labeling process")

	return nil
}
