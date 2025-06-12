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
	"strings"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeLabelInfo holds information about a node that matches the label criteria
type NodeLabelInfo struct {
	Name   string
	Labels map[string]string
}

// parseLabelSelector parses a label selector string that can be either "key" or "key=value"
func parseLabelSelector(selector string, logger *log.Entry) (string, string, bool) {
	logger.WithField("selector", selector).Debug("Parsing label selector")

	if strings.Contains(selector, "=") {
		parts := strings.SplitN(selector, "=", 2)
		logger.WithFields(log.Fields{
			"key":   parts[0],
			"value": parts[1],
		}).Debug("Parsed key=value selector")
		return parts[0], parts[1], true
	}

	logger.WithField("key", selector).Debug("Parsed key-only selector")
	return selector, "", false
}

// nodeMatchesLabelSelectors checks if a node matches any (rather than all) of the label selectors
// and excludes nodes that are already parked
func nodeMatchesLabelSelectors(node *v1.Node, labelSelectors []string, upgradeStatusLabel string, logger *log.Entry) bool {
	nodeLogger := logger.WithField("nodeName", node.Name)
	nodeLogger.Debug("Checking if node matches label selectors")

	nodeLabels := node.Labels
	if nodeLabels == nil {
		nodeLogger.Debug("Node has no labels")
		return false
	}

	// First check if the node is already parked - if so, exclude it
	if upgradeStatusLabel != "" {
		if upgradeStatus, exists := nodeLabels[upgradeStatusLabel]; exists && upgradeStatus == "parked" {
			nodeLogger.Debug("Node is already parked, excluding from selection")
			return false
		}
	}

	for _, selector := range labelSelectors {
		selectorLogger := nodeLogger.WithField("selector", selector)
		key, value, hasValue := parseLabelSelector(selector, selectorLogger)

		if nodeValue, exists := nodeLabels[key]; exists {
			if !hasValue {
				// If the selector is just a key, match if the key exists
				selectorLogger.WithField("nodeValue", nodeValue).Info("Node matches key-only selector")
				return true
			} else if nodeValue == value {
				// If the selector has a value, match if key=value
				selectorLogger.WithFields(log.Fields{
					"expectedValue": value,
					"nodeValue":     nodeValue,
				}).Info("Node matches key=value selector")
				return true
			} else {
				selectorLogger.WithFields(log.Fields{
					"expectedValue": value,
					"nodeValue":     nodeValue,
				}).Debug("Node value doesn't match selector value")
			}
		} else {
			selectorLogger.Debug("Node doesn't have the selector key")
		}
	}

	nodeLogger.Debug("Node doesn't match any label selectors")
	return false
}

// FindNodesWithLabels scans the kubernetes cluster for nodes that match the specified label selectors
// and excludes nodes that are already labeled as parked
func FindNodesWithLabels(ctx context.Context, k8sClient kubernetes.Interface, cfg config.Config, logger *log.Entry) ([]NodeLabelInfo, error) {
	logger = logger.WithField("function", "FindNodesWithLabels")

	if len(cfg.NodeLabelsToDetect) == 0 {
		logger.Debug("No node labels configured for detection")
		return []NodeLabelInfo{}, nil
	}

	logger.WithField("labelSelectors", cfg.NodeLabelsToDetect).Debug("Listing nodes with specified labels")

	// List all nodes (we'll filter them using an OR condition in nodeMatchesLabelSelectors)
	listOptions := metav1.ListOptions{}
	nodeList, err := k8sClient.CoreV1().Nodes().List(ctx, listOptions)
	if err != nil {
		logger.WithError(err).Error("Failed to list nodes")
		return nil, errors.Wrap(err, "failed to list nodes")
	}

	logger.WithField("totalNodes", len(nodeList.Items)).Debug("Retrieved nodes list")

	var matchingNodes []NodeLabelInfo

	for _, node := range nodeList.Items {
		// Check if the node matches any of the label selectors (this now also excludes already parked nodes)
		if nodeMatchesLabelSelectors(&node, cfg.NodeLabelsToDetect, cfg.UpgradeStatusLabel, logger) {
			logger.WithField("nodeName", node.Name).Info("Found node matching label criteria")

			matchingNodes = append(matchingNodes, NodeLabelInfo{
				Name:   node.Name,
				Labels: node.Labels,
			})
		}
	}

	logger.WithField("matchingCount", len(matchingNodes)).Info("Found nodes matching label criteria")

	return matchingNodes, nil
}

// ParkNodesWithLabels labels nodes that match the configured label selectors with the standard parking labels
func ParkNodesWithLabels(ctx context.Context, k8sClient kubernetes.Interface, matchingNodes []NodeLabelInfo, cfg config.Config, dryRun bool, logger *log.Entry) error {
	logger = logger.WithField("function", "ParkNodesWithLabels")

	logger.WithField("matchingNodesCount", len(matchingNodes)).Info("Starting to park nodes with labels")

	// Convert NodeLabelInfo to NodeInfo for the common parking function
	var nodesToPark []NodeInfo
	for _, nodeInfo := range matchingNodes {
		logger.WithField("nodeName", nodeInfo.Name).Debug("Adding node to parking list")
		nodesToPark = append(nodesToPark, NodeInfo(nodeInfo))
	}

	logger.WithField("nodesToPark", len(nodesToPark)).Info("Converted labeled nodes to parking list")

	// Use the common parking function
	return ParkNodes(ctx, k8sClient, nodesToPark, cfg, dryRun, "node-labels", logger)
}

// ProcessNodesWithLabels is the main function that combines finding nodes with specific labels and parking them
func ProcessNodesWithLabels(ctx context.Context, appContext *AppContext, logger *log.Entry) error {
	logger = logger.WithField("function", "ProcessNodesWithLabels")

	logger.Info("Starting node label detection and parking process")

	// Find nodes with specified labels
	matchingNodes, err := FindNodesWithLabels(ctx, appContext.K8sClient, appContext.Config, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to find nodes with specified labels")
		return errors.Wrap(err, "failed to find nodes with specified labels")
	}

	if len(matchingNodes) == 0 {
		logger.Info("No nodes found matching the specified label criteria")
		return nil
	}

	// Park the nodes that match the criteria
	err = ParkNodesWithLabels(ctx, appContext.K8sClient, matchingNodes, appContext.Config, appContext.IsDryRun(), logger)
	if err != nil {
		logger.WithError(err).Error("Failed to label nodes matching criteria")
		return errors.Wrap(err, "failed to label nodes matching criteria")
	}

	logger.WithField("processedNodes", len(matchingNodes)).Info("Completed node label detection and parking process")

	return nil
}
