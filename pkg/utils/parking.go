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
