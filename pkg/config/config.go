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

package config

import "time"

// Config struct defines application configuration options
type Config struct {
	// EvictionLoopInterval defines how often to run the eviction loop process
	EvictionLoopInterval time.Duration
	// ParkedNodeTTL is used for defining the time a node can stay parked before starting force eviction process
	ParkedNodeTTL time.Duration
	// RollingRestartThreshold specifies how much time(percentage) should pass from ParkedNodeTTL before starting the rollout restart process
	RollingRestartThreshold float64
	// UpgradeStatusLabel is used for identifying parked nodes
	UpgradeStatusLabel string
	// ExpiresOnLabel is used for identifying the TTL for parked nodes
	ExpiresOnLabel string
	// NamespacePrefixSkipInitialEviction is used for proceeding directly with a rollout restart without waiting for the RollingRestartThreshold
	NamespacePrefixSkipInitialEviction string
	// RestartedAtAnnotation is used to mark a controller object for rollout restart
	RestartedAtAnnotation string
	// AllowEvictionLabel is used for skipping evicting pods that have explicitly set this label on false
	AllowEvictionLabel string
	// ToBeDeletedTaint is used for skipping a subset of parked nodes
	ToBeDeletedTaint string
	// ArgoRolloutsAPIVersion is used for specifying the API version from `argoproj.io` apigroup to be used while handling Argo Rollouts objects
	ArgoRolloutsAPIVersion string
	// EnableKarpenterDriftDetection controls whether to scan for drifted Karpenter NodeClaims and automatically label their nodes
	EnableKarpenterDriftDetection bool
	// EnableKarpenterDisruptionDetection controls whether to scan for disrupted Karpenter NodeClaims and automatically label their nodes
	EnableKarpenterDisruptionDetection bool
	// ParkedByLabel is used for identifying which component parked the node
	ParkedByLabel string
	// ParkedByValue is the value to set for the ParkedByLabel
	ParkedByValue string
	// ParkedNodeTaint is the taint to apply to parked nodes in the format key=value:effect
	ParkedNodeTaint string
	// EnableNodeLabelDetection controls whether to scan for nodes with specific labels and automatically park them
	EnableNodeLabelDetection bool
	// NodeLabelsToDetect is a list of node labels to look for. Can be just keys or key=value pairs
	NodeLabelsToDetect []string
	// MaxParkedNodes is the maximum number of nodes that can be parked simultaneously. If set to 0 (default), no limit is applied.
	MaxParkedNodes int
	// ExtraParkingLabels is a map of additional labels to apply to nodes and pods during the parking process. If not set, no extra labels are applied.
	ExtraParkingLabels map[string]string
	// EvictionSafetyCheck controls whether to perform safety checks before force eviction. If true, nodes will be unparked if pods don't have required parking labels.
	EvictionSafetyCheck bool
	// ParkingReasonLabel is the label used to track why a node or pod was parked
	ParkingReasonLabel string
}
