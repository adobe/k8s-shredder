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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (

	// ShredderAPIServerRequestsTotal = Total requests for Kubernetes API
	ShredderAPIServerRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shredder_apiserver_requests_total",
			Help: "Total requests for Kubernetes API",
		},
		[]string{"verb", "resource", "status"},
	)

	// ShredderAPIServerRequestsDurationSeconds = Requests duration seconds for calling Kubernetes API
	ShredderAPIServerRequestsDurationSeconds = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "shredder_apiserver_requests_duration_seconds",
			Help:       "Requests duration when calling Kubernetes API",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"verb", "resource", "status"},
	)

	// ShredderLoopsTotal = Total loops
	ShredderLoopsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_loops_total",
			Help: "Total loops",
		},
	)

	// ShredderLoopsDurationSeconds = Loops duration in seconds
	ShredderLoopsDurationSeconds = prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name:       "shredder_loops_duration_seconds",
			Help:       "Loops duration in seconds",
			Objectives: map[float64]float64{0.5: 1200, 0.9: 900, 0.99: 600},
		},
	)

	// ShredderProcessedNodesTotal = Total processed nodes
	ShredderProcessedNodesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_processed_nodes_total",
			Help: "Total processed nodes",
		},
	)

	// ShredderProcessedPodsTotal = Total processed pods
	ShredderProcessedPodsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_processed_pods_total",
			Help: "Total processed pods",
		},
	)

	// ShredderErrorsTotal = Total errors
	ShredderErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_errors_total",
			Help: "Total errors",
		},
	)

	// ShredderPodErrorsTotal = Total pod errors
	ShredderPodErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shredder_pod_errors_total",
			Help: "Total pod errors",
		},
		[]string{"pod_name", "namespace", "reason", "action"},
	)

	// ShredderNodeForceToEvictTime = Time when the node will be forcibly evicted
	ShredderNodeForceToEvictTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shredder_node_force_to_evict_time",
			Help: "Time when the node will be forcibly evicted",
		},
		[]string{"node_name"},
	)

	// ShredderPodForceToEvictTime = Time when the pod will be forcibly evicted
	ShredderPodForceToEvictTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shredder_pod_force_to_evict_time",
			Help: "Time when the pod will be forcibly evicted",
		},
		[]string{"pod_name", "namespace"},
	)
)
