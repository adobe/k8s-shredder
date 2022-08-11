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
}
