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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// NodeInfo represents a node that needs to be parked
type NodeInfo struct {
	Name   string
	Labels map[string]string
}

// ParkingLabels holds all the labels to be applied when parking nodes and pods
type ParkingLabels struct {
	UpgradeStatusLabel string
	UpgradeStatusValue string
	ExpiresOnLabel     string
	ExpiresOnValue     string
	ParkedByLabel      string
	ParkedByValue      string
	ParkingReasonLabel string
	ParkingReasonValue string
	ExtraLabels        map[string]string // Extra labels to apply to nodes and pods
}

// isNodeAlreadyParked checks if a node is already labeled with the parked status
func isNodeAlreadyParked(ctx context.Context, k8sClient kubernetes.Interface, nodeName, upgradeStatusLabel string, logger *log.Entry) (bool, error) {
	logger.WithField("nodeName", nodeName).Debug("Checking if node is already parked")

	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		logger.WithField("nodeName", nodeName).WithError(err).Error("Failed to get node")
		return false, errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	if node.Labels == nil {
		logger.WithField("nodeName", nodeName).Debug("Node has no labels")
		return false, nil
	}

	upgradeStatus, exists := node.Labels[upgradeStatusLabel]
	isParked := exists && upgradeStatus == "parked"

	logger.WithFields(log.Fields{
		"nodeName": nodeName,
		"isParked": isParked,
	}).Debug("Node parking status checked")

	return isParked, nil
}

// getEligiblePodsForNode returns all eligible for labeling pods from a specific node (excluding DaemonSet and static pods)
func getEligiblePodsForNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName string, logger *log.Entry) ([]v1.Pod, error) {
	logger.WithField("nodeName", nodeName).Debug("Getting eligible pods for node")

	podList, err := k8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})

	if err != nil {
		logger.WithField("nodeName", nodeName).WithError(err).Error("Failed to list pods for node")
		return nil, err
	}

	var podListCleaned []v1.Pod

	// we need to remove any non-eligible pods
	for _, pod := range podList.Items {
		// skip pods in terminating state
		if pod.DeletionTimestamp != nil {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"nodeName":  nodeName,
			}).Debug("Skipping pod in terminating state")
			continue
		}

		// skip pods with DaemonSet controller object or static pods
		if len(pod.OwnerReferences) > 0 && slices.Contains([]string{"DaemonSet", "Node"}, pod.ObjectMeta.OwnerReferences[0].Kind) {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"nodeName":  nodeName,
				"ownerKind": pod.ObjectMeta.OwnerReferences[0].Kind,
			}).Debug("Skipping DaemonSet or static pod")
			continue
		}

		podListCleaned = append(podListCleaned, pod)
	}

	logger.WithFields(log.Fields{
		"nodeName":     nodeName,
		"totalPods":    len(podList.Items),
		"eligiblePods": len(podListCleaned),
	}).Debug("Found eligible pods for node")

	return podListCleaned, nil
}

// parseTaintString parses a taint string in the format key=value:effect and returns the components
func parseTaintString(taintStr string) (string, string, v1.TaintEffect, error) {
	// Split by colon to separate key=value from effect
	parts := strings.Split(taintStr, ":")
	if len(parts) != 2 {
		return "", "", "", errors.Errorf("invalid taint format, expected key=value:effect, got %s", taintStr)
	}

	effect := v1.TaintEffect(parts[1])

	// Split key=value part
	keyValueParts := strings.Split(parts[0], "=")
	if len(keyValueParts) != 2 {
		return "", "", "", errors.Errorf("invalid taint key=value format, expected key=value, got %s", parts[0])
	}

	key := keyValueParts[0]
	value := keyValueParts[1]

	// Validate effect
	validEffects := []v1.TaintEffect{v1.TaintEffectNoSchedule, v1.TaintEffectPreferNoSchedule, v1.TaintEffectNoExecute}
	if !slices.Contains(validEffects, effect) {
		return "", "", "", errors.Errorf("invalid taint effect %s, must be one of: NoSchedule, PreferNoSchedule, NoExecute", effect)
	}

	return key, value, effect, nil
}

// cordonAndTaintNode cordons a node and applies the specified taint
func cordonAndTaintNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName, taintStr string, dryRun bool, logger *log.Entry) error {
	logger = logger.WithFields(log.Fields{
		"node":     nodeName,
		"taintStr": taintStr,
		"dryRun":   dryRun,
	})

	logger.Debug("Starting cordon and taint operation")

	// Parse the taint string
	taintKey, taintValue, taintEffect, err := parseTaintString(taintStr)
	if err != nil {
		logger.WithError(err).Error("Failed to parse taint string")
		return errors.Wrap(err, "failed to parse taint string")
	}

	// Get the node
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to get node")
		return errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	// Check if node is already cordoned and has the taint
	alreadyCordoned := node.Spec.Unschedulable
	alreadyTainted := false

	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Value == taintValue && taint.Effect == taintEffect {
			alreadyTainted = true
			break
		}
	}

	if alreadyCordoned && alreadyTainted {
		logger.Debug("Node is already cordoned and tainted, skipping")
		return nil
	}

	// Cordon the node
	if !alreadyCordoned {
		node.Spec.Unschedulable = true
		logger.Info("Cordoning node")
	}

	// Add the taint if not already present
	if !alreadyTainted {
		newTaint := v1.Taint{
			Key:    taintKey,
			Value:  taintValue,
			Effect: taintEffect,
		}
		node.Spec.Taints = append(node.Spec.Taints, newTaint)
		logger.WithFields(log.Fields{
			"taintKey":    taintKey,
			"taintValue":  taintValue,
			"taintEffect": taintEffect,
		}).Info("Adding taint to node")
	}

	if dryRun {
		logger.Info("DRY-RUN: Would cordon and taint node")
		return nil
	}

	// Update the node
	_, err = k8sClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to update node with cordon and taint")
		return errors.Wrapf(err, "failed to update node %s with cordon and taint", nodeName)
	}

	logger.Info("Node cordoned and tainted successfully")
	return nil
}

// labelNode applies the specified labels to a node
func labelNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName string, labels ParkingLabels, dryRun bool, logger *log.Entry) error {
	logger = logger.WithFields(log.Fields{
		"node":               nodeName,
		"upgradeStatusLabel": labels.UpgradeStatusLabel,
		"upgradeStatusValue": labels.UpgradeStatusValue,
		"expiresOnLabel":     labels.ExpiresOnLabel,
		"expiresOnValue":     labels.ExpiresOnValue,
		"parkedByLabel":      labels.ParkedByLabel,
		"parkedByValue":      labels.ParkedByValue,
		"parkingReasonLabel": labels.ParkingReasonLabel,
		"parkingReasonValue": labels.ParkingReasonValue,
		"extraLabels":        labels.ExtraLabels,
		"dryRun":             dryRun,
	})

	logger.Debug("Starting node labeling operation")

	// Get the node first
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to get node")
		return errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	// Check if the node already has the labels
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	existingUpgradeStatus := node.Labels[labels.UpgradeStatusLabel]
	existingExpiresOn := node.Labels[labels.ExpiresOnLabel]

	// Check if the node is already labeled as parked
	if existingUpgradeStatus == "parked" && existingExpiresOn != "" {
		logger.Debug("Node is already labeled as parked, skipping")
		return nil
	}

	// Apply the labels
	node.Labels[labels.UpgradeStatusLabel] = labels.UpgradeStatusValue
	node.Labels[labels.ExpiresOnLabel] = labels.ExpiresOnValue
	node.Labels[labels.ParkedByLabel] = labels.ParkedByValue
	node.Labels[labels.ParkingReasonLabel] = labels.ParkingReasonValue
	// Apply extra labels
	for k, v := range labels.ExtraLabels {
		node.Labels[k] = v
	}

	if dryRun {
		logger.Info("DRY-RUN: Would label node")
		return nil
	}

	// Update the node
	_, err = k8sClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to update node with labels")
		return errors.Wrapf(err, "failed to update node %s with labels", nodeName)
	}

	logger.Info("Node labeled successfully")
	return nil
}

// labelPod applies the specified labels to a pod
func labelPod(ctx context.Context, k8sClient kubernetes.Interface, pod v1.Pod, labels ParkingLabels, dryRun bool, logger *log.Entry) error {
	logger = logger.WithFields(log.Fields{
		"pod":                pod.Name,
		"namespace":          pod.Namespace,
		"upgradeStatusLabel": labels.UpgradeStatusLabel,
		"upgradeStatusValue": labels.UpgradeStatusValue,
		"expiresOnLabel":     labels.ExpiresOnLabel,
		"expiresOnValue":     labels.ExpiresOnValue,
		"parkedByLabel":      labels.ParkedByLabel,
		"parkedByValue":      labels.ParkedByValue,
		"parkingReasonLabel": labels.ParkingReasonLabel,
		"parkingReasonValue": labels.ParkingReasonValue,
		"extraLabels":        labels.ExtraLabels,
		"dryRun":             dryRun,
	})

	logger.Debug("Starting pod labeling operation")

	// Get the pod first to check current labels
	currentPod, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to get pod")
		return errors.Wrapf(err, "failed to get pod %s/%s", pod.Namespace, pod.Name)
	}

	// Check if the pod already has the labels
	if currentPod.Labels == nil {
		currentPod.Labels = make(map[string]string)
	}

	existingUpgradeStatus := currentPod.Labels[labels.UpgradeStatusLabel]
	existingExpiresOn := currentPod.Labels[labels.ExpiresOnLabel]

	// Check if the pod is already labeled as parked
	if existingUpgradeStatus == "parked" && existingExpiresOn != "" {
		logger.Debug("Pod is already labeled as parked, skipping")
		return nil
	}

	// Apply the labels
	currentPod.Labels[labels.UpgradeStatusLabel] = labels.UpgradeStatusValue
	currentPod.Labels[labels.ExpiresOnLabel] = labels.ExpiresOnValue
	currentPod.Labels[labels.ParkedByLabel] = labels.ParkedByValue
	currentPod.Labels[labels.ParkingReasonLabel] = labels.ParkingReasonValue
	// Apply extra labels
	for k, v := range labels.ExtraLabels {
		currentPod.Labels[k] = v
	}

	if dryRun {
		logger.Info("DRY-RUN: Would label pod")
		return nil
	}

	// Update the pod
	_, err = k8sClient.CoreV1().Pods(pod.Namespace).Update(ctx, currentPod, metav1.UpdateOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to update pod with labels")
		return errors.Wrapf(err, "failed to update pod %s/%s with labels", pod.Namespace, pod.Name)
	}

	logger.Debug("Pod labeled successfully")
	return nil
}

// ParkNodes labels, cordons, taints nodes and labels their pods with parking labels
// This is the unified function that both Karpenter drift detection and node label detection use
func ParkNodes(ctx context.Context, k8sClient kubernetes.Interface, nodes []NodeInfo, cfg config.Config, dryRun bool, source string, logger *log.Entry) error {
	logger = logger.WithField("function", "ParkNodes").WithField("source", source)

	if len(nodes) == 0 {
		logger.Debug("No nodes to park")
		return nil
	}

	// Calculate the expiration time
	expirationTime := time.Now().Add(cfg.ParkedNodeTTL)
	expirationUnixTime := strconv.FormatInt(expirationTime.Unix(), 10)

	logger.WithFields(log.Fields{
		"upgradeStatusLabel": cfg.UpgradeStatusLabel,
		"expiresOnLabel":     cfg.ExpiresOnLabel,
		"expirationTime":     expirationTime.Format(time.RFC3339),
		"dryRun":             dryRun,
		"nodeCount":          len(nodes),
	}).Info("Starting to park nodes")

	// Create parking labels struct once to reuse for all nodes and pods
	parkingLabels := ParkingLabels{
		UpgradeStatusLabel: cfg.UpgradeStatusLabel,
		UpgradeStatusValue: "parked",
		ExpiresOnLabel:     cfg.ExpiresOnLabel,
		ExpiresOnValue:     expirationUnixTime,
		ParkedByLabel:      cfg.ParkedByLabel,
		ParkedByValue:      cfg.ParkedByValue,
		ParkingReasonLabel: cfg.ParkingReasonLabel,
		ParkingReasonValue: source,
		ExtraLabels:        cfg.ExtraParkingLabels,
	}

	for _, nodeInfo := range nodes {
		if nodeInfo.Name == "" {
			logger.Warn("Node has no name, skipping")
			continue
		}

		nodeLogger := logger.WithField("nodeName", nodeInfo.Name)

		// Label the node
		err := labelNode(ctx, k8sClient, nodeInfo.Name, parkingLabels, dryRun, nodeLogger)
		if err != nil {
			nodeLogger.WithError(err).Error("Failed to label node")
			continue
		}

		// Cordon and taint the node
		err = cordonAndTaintNode(ctx, k8sClient, nodeInfo.Name, cfg.ParkedNodeTaint, dryRun, nodeLogger)
		if err != nil {
			nodeLogger.WithError(err).Error("Failed to cordon and taint node")
			// Continue with pod labeling even if cordoning/tainting fails
		}

		nodeLogger.Info("Successfully processed node (labeled, cordoned, and tainted)")

		// Get eligible pods on the node and label them
		pods, err := getEligiblePodsForNode(ctx, k8sClient, nodeInfo.Name, nodeLogger)
		if err != nil {
			nodeLogger.WithError(err).Error("Failed to get pods for node")
			continue
		}

		nodeLogger.WithFields(log.Fields{
			"nodeName": nodeInfo.Name,
			"podCount": len(pods),
		}).Info("Found eligible pods to label on node")

		// Label each eligible pod
		for _, pod := range pods {
			podLogger := nodeLogger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
			})

			err := labelPod(ctx, k8sClient, pod, parkingLabels, dryRun, podLogger)
			if err != nil {
				podLogger.WithError(err).Error("Failed to label pod")
				continue
			}

			podLogger.Debug("Successfully labeled pod on node")
		}
	}

	logger.WithField("processedNodes", len(nodes)).Info("Completed parking process")
	return nil
}

// CountParkedNodes returns the number of nodes currently labeled as parked
func CountParkedNodes(ctx context.Context, k8sClient kubernetes.Interface, upgradeStatusLabel string, logger *log.Entry) (int, error) {
	logger = logger.WithField("function", "CountParkedNodes")

	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			upgradeStatusLabel: "parked",
		},
	}

	nodeList, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})

	if err != nil {
		logger.WithError(err).Error("Failed to list parked nodes")
		return 0, errors.Wrap(err, "failed to list parked nodes")
	}

	count := len(nodeList.Items)
	logger.WithField("parkedNodesCount", count).Debug("Counted currently parked nodes")

	return count, nil
}

// ParseMaxParkedNodes parses the MaxParkedNodes configuration and returns the actual limit
// It supports both integer values (e.g., "5") and percentage values (e.g., "20%")
// For percentage values, it calculates the limit as (percentage/100) * totalNodes
// Returns 0 for invalid values or when no limit should be applied
func ParseMaxParkedNodes(ctx context.Context, k8sClient kubernetes.Interface, maxParkedNodesStr string, logger *log.Entry) (int, error) {
	logger = logger.WithField("function", "ParseMaxParkedNodes")

	// Handle empty or "0" values
	if maxParkedNodesStr == "" || maxParkedNodesStr == "0" {
		logger.Debug("MaxParkedNodes is empty or 0, no limit will be applied")
		return 0, nil
	}

	// Check if it's a percentage
	if strings.HasSuffix(maxParkedNodesStr, "%") {
		// Parse percentage value
		percentageStr := strings.TrimSuffix(maxParkedNodesStr, "%")
		percentage, err := strconv.ParseFloat(percentageStr, 64)
		if err != nil {
			logger.WithError(err).WithField("value", maxParkedNodesStr).Warn("Failed to parse MaxParkedNodes percentage, treating as 0 (no limit)")
			return 0, nil
		}

		if percentage < 0 {
			logger.WithField("percentage", percentage).Warn("MaxParkedNodes percentage is negative, treating as 0 (no limit)")
			return 0, nil
		}

		if percentage == 0 {
			logger.Debug("MaxParkedNodes percentage is 0, no limit will be applied")
			return 0, nil
		}

		// Get total number of nodes in the cluster
		nodeList, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			logger.WithError(err).Error("Failed to list nodes for percentage calculation")
			return 0, errors.Wrap(err, "failed to list nodes for percentage calculation")
		}

		totalNodes := len(nodeList.Items)
		if totalNodes == 0 {
			logger.Warn("No nodes found in cluster, cannot calculate percentage-based limit")
			return 0, nil
		}

		// Calculate the limit: (percentage/100) * totalNodes, rounded down
		limit := int((percentage / 100.0) * float64(totalNodes))

		logger.WithFields(log.Fields{
			"percentage": percentage,
			"totalNodes": totalNodes,
			"limit":      limit,
		}).Info("Calculated MaxParkedNodes limit from percentage")

		return limit, nil
	}

	// Not a percentage, try to parse as integer
	limit, err := strconv.Atoi(maxParkedNodesStr)
	if err != nil {
		logger.WithError(err).WithField("value", maxParkedNodesStr).Warn("Failed to parse MaxParkedNodes as integer, treating as 0 (no limit)")
		return 0, nil
	}

	if limit < 0 {
		logger.WithField("limit", limit).Warn("MaxParkedNodes is negative, treating as 0 (no limit)")
		return 0, nil
	}

	logger.WithField("limit", limit).Debug("Using MaxParkedNodes as integer limit")
	return limit, nil
}

// LimitNodesToPark limits the number of nodes to park based on MaxParkedNodes configuration
// It returns the nodes that should be parked, prioritizing the oldest nodes first
func LimitNodesToPark(ctx context.Context, k8sClient kubernetes.Interface, nodes []NodeInfo, maxParkedNodesStr string, upgradeStatusLabel string, logger *log.Entry) ([]NodeInfo, error) {
	logger = logger.WithField("function", "LimitNodesToPark")

	// Parse MaxParkedNodes to get the actual limit
	maxParkedNodes, err := ParseMaxParkedNodes(ctx, k8sClient, maxParkedNodesStr, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to parse MaxParkedNodes")
		return nil, errors.Wrap(err, "failed to parse MaxParkedNodes")
	}

	if maxParkedNodes <= 0 {
		logger.Debug("MaxParkedNodes is not set or invalid, parking all eligible nodes")
		return nodes, nil
	}

	// Count currently parked nodes
	currentlyParked, err := CountParkedNodes(ctx, k8sClient, upgradeStatusLabel, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to count currently parked nodes")
		return nil, errors.Wrap(err, "failed to count currently parked nodes")
	}

	logger.WithFields(log.Fields{
		"currentlyParked":   currentlyParked,
		"maxParkedNodes":    maxParkedNodes,
		"maxParkedNodesStr": maxParkedNodesStr,
		"eligibleNodes":     len(nodes),
	}).Info("Checking parking limits")

	// Calculate how many nodes we can park
	availableSlots := maxParkedNodes - currentlyParked
	if availableSlots <= 0 {
		logger.WithFields(log.Fields{
			"currentlyParked":   currentlyParked,
			"maxParkedNodes":    maxParkedNodes,
			"maxParkedNodesStr": maxParkedNodesStr,
			"availableSlots":    availableSlots,
		}).Warn("No available parking slots, skipping parking for this interval")
		return []NodeInfo{}, nil
	}

	// If we have more eligible nodes than available slots, limit to the oldest nodes
	if len(nodes) > availableSlots {
		logger.WithFields(log.Fields{
			"eligibleNodes":  len(nodes),
			"availableSlots": availableSlots,
			"nodesToPark":    availableSlots,
			"nodesToSkip":    len(nodes) - availableSlots,
		}).Info("Limiting nodes to park based on MaxParkedNodes configuration")

		// For now, we'll take the first availableSlots nodes
		// In a future enhancement, we could sort by node creation time or other criteria
		limitedNodes := nodes[:availableSlots]

		// Log which nodes are being skipped
		for i := availableSlots; i < len(nodes); i++ {
			logger.WithField("skippedNode", nodes[i].Name).Debug("Skipping node due to MaxParkedNodes limit")
		}

		return limitedNodes, nil
	}

	logger.WithFields(log.Fields{
		"eligibleNodes":  len(nodes),
		"availableSlots": availableSlots,
	}).Info("All eligible nodes can be parked within MaxParkedNodes limit")

	return nodes, nil
}

// UnparkNode unparks a node by removing parking labels, taints, and uncordoning it
func UnparkNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName string, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithFields(log.Fields{
		"node":   nodeName,
		"dryRun": dryRun,
	})

	logger.Info("Starting node unparking process")

	// Get the node
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to get node")
		return errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	// Check if node is actually parked
	if node.Labels == nil {
		logger.Debug("Node has no labels, nothing to unpark")
		return nil
	}

	upgradeStatus, exists := node.Labels[cfg.UpgradeStatusLabel]
	if !exists || upgradeStatus != "parked" {
		logger.Debug("Node is not parked, nothing to unpark")
		return nil
	}

	// Get eligible pods for unparking
	pods, err := getEligiblePodsForNode(ctx, k8sClient, nodeName, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to get eligible pods for node")
		return errors.Wrapf(err, "failed to get eligible pods for node %s", nodeName)
	}

	// Unpark pods first
	for _, pod := range pods {
		err = UnparkPod(ctx, k8sClient, pod, cfg, dryRun, logger)
		if err != nil {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
			}).WithError(err).Warn("Failed to unpark pod, continuing with other pods")
			// Continue with other pods even if one fails
		}
	}

	// Unpark the node
	err = unparkNodeObject(ctx, k8sClient, node, cfg, dryRun, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to unpark node")
		return errors.Wrapf(err, "failed to unpark node %s", nodeName)
	}

	logger.Info("Node unparking completed successfully")
	return nil
}

// UnparkPod unparks a pod by removing parking labels
func UnparkPod(ctx context.Context, k8sClient kubernetes.Interface, pod v1.Pod, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithFields(log.Fields{
		"pod":       pod.Name,
		"namespace": pod.Namespace,
		"dryRun":    dryRun,
	})

	logger.Debug("Starting pod unparking process")

	// Check if pod has parking labels
	if pod.Labels == nil {
		logger.Debug("Pod has no labels, nothing to unpark")
		return nil
	}

	upgradeStatus, exists := pod.Labels[cfg.UpgradeStatusLabel]
	if !exists || upgradeStatus != "parked" {
		logger.Debug("Pod is not parked, nothing to unpark")
		return nil
	}

	// Create a copy of the pod for modification
	podCopy := pod.DeepCopy()

	// Remove parking labels
	if podCopy.Labels != nil {
		// Remove UpgradeStatusLabel
		delete(podCopy.Labels, cfg.UpgradeStatusLabel)

		// Remove ExpiresOnLabel
		delete(podCopy.Labels, cfg.ExpiresOnLabel)

		// Remove ParkedByLabel
		delete(podCopy.Labels, cfg.ParkedByLabel)

		// Remove ParkingReasonLabel
		delete(podCopy.Labels, cfg.ParkingReasonLabel)

		// Remove ExtraParkingLabels
		for key := range cfg.ExtraParkingLabels {
			delete(podCopy.Labels, key)
		}

		// Set UpgradeStatusLabel to "unparked"
		podCopy.Labels[cfg.UpgradeStatusLabel] = "unparked"

		// Set ParkedByLabel to ParkedByValue
		podCopy.Labels[cfg.ParkedByLabel] = cfg.ParkedByValue
	}

	if dryRun {
		logger.Info("DRY RUN: Would unpark pod")
		return nil
	}

	// Update the pod
	_, err := k8sClient.CoreV1().Pods(pod.Namespace).Update(ctx, podCopy, metav1.UpdateOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to update pod")
		return errors.Wrapf(err, "failed to update pod %s in namespace %s", pod.Name, pod.Namespace)
	}

	logger.Info("Pod unparked successfully")
	return nil
}

// unparkNodeObject handles the actual node object unparking (labels, taints, cordon)
func unparkNodeObject(ctx context.Context, k8sClient kubernetes.Interface, node *v1.Node, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithField("node", node.Name)

	// Create a copy of the node for modification
	nodeCopy := node.DeepCopy()

	// Remove parking labels
	if nodeCopy.Labels != nil {
		// Remove UpgradeStatusLabel
		delete(nodeCopy.Labels, cfg.UpgradeStatusLabel)

		// Remove ExpiresOnLabel
		delete(nodeCopy.Labels, cfg.ExpiresOnLabel)

		// Remove ParkedByLabel
		delete(nodeCopy.Labels, cfg.ParkedByLabel)

		// Remove ParkingReasonLabel
		delete(nodeCopy.Labels, cfg.ParkingReasonLabel)

		// Remove ExtraParkingLabels
		for key := range cfg.ExtraParkingLabels {
			delete(nodeCopy.Labels, key)
		}

		// Set UpgradeStatusLabel to "unparked"
		nodeCopy.Labels[cfg.UpgradeStatusLabel] = "unparked"

		// Set ParkedByLabel to ParkedByValue
		nodeCopy.Labels[cfg.ParkedByLabel] = cfg.ParkedByValue
	}

	// Remove parking taint
	if cfg.ParkedNodeTaint != "" {
		taintKey, taintValue, taintEffect, err := parseTaintString(cfg.ParkedNodeTaint)
		if err != nil {
			logger.WithError(err).Warn("Failed to parse parking taint, skipping taint removal")
		} else {
			// Remove the taint
			var newTaints []v1.Taint
			for _, taint := range nodeCopy.Spec.Taints {
				if taint.Key != taintKey || taint.Value != taintValue || taint.Effect != taintEffect {
					newTaints = append(newTaints, taint)
				}
			}
			nodeCopy.Spec.Taints = newTaints
			logger.Debug("Removed parking taint from node")
		}
	}

	// Uncordon the node
	if nodeCopy.Spec.Unschedulable {
		nodeCopy.Spec.Unschedulable = false
		logger.Debug("Uncordoning node")
	}

	if dryRun {
		logger.Info("DRY RUN: Would unpark node")
		return nil
	}

	// Update the node
	_, err := k8sClient.CoreV1().Nodes().Update(ctx, nodeCopy, metav1.UpdateOptions{})
	if err != nil {
		logger.WithError(err).Error("Failed to update node")
		return errors.Wrapf(err, "failed to update node %s", node.Name)
	}

	logger.Info("Node object unparked successfully")
	return nil
}

// CheckPodParkingSafety checks if all eligible pods on a node have the required parking labels
func CheckPodParkingSafety(ctx context.Context, k8sClient kubernetes.Interface, nodeName string, cfg config.Config, logger *log.Entry) (bool, error) {
	logger = logger.WithField("node", nodeName)

	logger.Debug("Checking pod parking safety")

	// Get eligible pods for the node
	pods, err := getEligiblePodsForNode(ctx, k8sClient, nodeName, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to get eligible pods for node")
		return false, errors.Wrapf(err, "failed to get eligible pods for node %s", nodeName)
	}

	if len(pods) == 0 {
		logger.Debug("No eligible pods found on node (only DaemonSet/static pods remain), safety check passes")
		return true, nil
	}

	// Check each pod for required parking labels
	for _, pod := range pods {
		if pod.Labels == nil {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
			}).Debug("Pod has no labels, safety check fails")
			return false, nil
		}

		// Check UpgradeStatusLabel
		upgradeStatus, exists := pod.Labels[cfg.UpgradeStatusLabel]
		if !exists || upgradeStatus != "parked" {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"label":     cfg.UpgradeStatusLabel,
				"value":     upgradeStatus,
				"exists":    exists,
			}).Debug("Pod missing or has incorrect UpgradeStatusLabel, safety check fails")
			return false, nil
		}

		// Check ExpiresOnLabel
		_, exists = pod.Labels[cfg.ExpiresOnLabel]
		if !exists {
			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"label":     cfg.ExpiresOnLabel,
			}).Debug("Pod missing ExpiresOnLabel, safety check fails")
			return false, nil
		}
	}

	logger.Debug("All eligible pods have required parking labels, safety check passes")
	return true, nil
}
