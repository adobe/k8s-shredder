# k8s-shredder Metrics

This document describes all the metrics exposed by k8s-shredder. These metrics are available at the `/metrics` endpoint and can be scraped by Prometheus or other monitoring systems.

## Overview

k8s-shredder exposes metrics in Prometheus format to help operators monitor the health and performance of the node parking and eviction processes. The metrics are organized into several categories:

- **Core Operation Metrics**: General operation counters and timing
- **API Server Metrics**: Kubernetes API interaction metrics
- **Node Processing Metrics**: Node parking and processing statistics
- **Pod Processing Metrics**: Pod eviction and processing statistics
- **Karpenter Integration Metrics**: Karpenter drift detection metrics
- **Node Label Detection Metrics**: Node label-based detection metrics
- **Shared Metrics**: Aggregated metrics across all detection methods

## Core Operation Metrics

### `shredder_loops_total`
- **Type**: Counter
- **Description**: Total number of eviction loops completed
- **Use Case**: Monitor the frequency of eviction loop execution and overall system activity

### `shredder_loops_duration_seconds`
- **Type**: Summary
- **Description**: Duration of eviction loops in seconds
- **Objectives**: 0.5: 1200, 0.9: 900, 0.99: 600
- **Use Case**: Monitor the performance of eviction loops and identify slow operations

### `shredder_errors_total`
- **Type**: Counter
- **Description**: Total number of errors encountered during operation
- **Use Case**: Monitor system health and identify operational issues

## API Server Metrics

### `shredder_apiserver_requests_total`
- **Type**: Counter Vector
- **Labels**: `verb`, `resource`, `status`
- **Description**: Total requests made to the Kubernetes API
- **Use Case**: Monitor API usage patterns and identify potential rate limiting issues

### `shredder_apiserver_requests_duration_seconds`
- **Type**: Summary Vector
- **Labels**: `verb`, `resource`, `status`
- **Description**: Duration of Kubernetes API requests in seconds
- **Objectives**: 0.5: 0.05, 0.9: 0.01, 0.99: 0.001
- **Use Case**: Monitor API performance and identify slow API calls

## Node Processing Metrics

### `shredder_processed_nodes_total`
- **Type**: Counter
- **Description**: Total number of nodes processed during eviction loops
- **Use Case**: Monitor the volume of node processing activity

### `shredder_node_force_to_evict_time`
- **Type**: Gauge Vector
- **Labels**: `node_name`
- **Description**: Unix timestamp when a node will be forcibly evicted
- **Use Case**: Monitor when nodes are scheduled for forced eviction

## Pod Processing Metrics

### `shredder_processed_pods_total`
- **Type**: Counter
- **Description**: Total number of pods processed during eviction loops
- **Use Case**: Monitor the volume of pod processing activity

### `shredder_pod_errors_total`
- **Type**: Gauge Vector
- **Labels**: `pod_name`, `namespace`, `reason`, `action`
- **Description**: Total pod errors per eviction loop
- **Use Case**: Monitor pod eviction failures and their reasons

### `shredder_pod_force_to_evict_time`
- **Type**: Gauge Vector
- **Labels**: `pod_name`, `namespace`
- **Description**: Unix timestamp when a pod will be forcibly evicted
- **Use Case**: Monitor when pods are scheduled for forced eviction

## Karpenter Integration Metrics

### `shredder_karpenter_drifted_nodes_total`
- **Type**: Counter
- **Description**: Total number of drifted Karpenter nodes detected
- **Use Case**: Monitor the volume of Karpenter drift detection activity

### `shredder_karpenter_disrupted_nodes_total`
- **Type**: Counter
- **Description**: Total number of disrupted Karpenter nodes detected
- **Use Case**: Monitor the volume of Karpenter disruption detection activity

### `shredder_karpenter_nodes_parked_total`
- **Type**: Counter
- **Description**: Total number of Karpenter nodes successfully parked
- **Use Case**: Monitor successful Karpenter node parking operations

### `shredder_karpenter_nodes_parking_failed_total`
- **Type**: Counter
- **Description**: Total number of Karpenter nodes that failed to be parked
- **Use Case**: Monitor Karpenter node parking failures

### `shredder_karpenter_processing_duration_seconds`
- **Type**: Summary
- **Description**: Duration of Karpenter node processing in seconds
- **Objectives**: 0.5: 0.05, 0.9: 0.01, 0.99: 0.001
- **Use Case**: Monitor the performance of Karpenter drift detection and parking operations

## Node Label Detection Metrics

### `shredder_node_label_nodes_parked_total`
- **Type**: Counter
- **Description**: Total number of nodes successfully parked via node label detection
- **Use Case**: Monitor successful node label-based parking operations

### `shredder_node_label_nodes_parking_failed_total`
- **Type**: Counter
- **Description**: Total number of nodes that failed to be parked via node label detection
- **Use Case**: Monitor node label-based parking failures

### `shredder_node_label_processing_duration_seconds`
- **Type**: Summary
- **Description**: Duration of node label detection and parking process in seconds
- **Objectives**: 0.5: 0.05, 0.9: 0.01, 0.99: 0.001
- **Use Case**: Monitor the performance of node label detection and parking operations

### `shredder_node_label_matching_nodes_total`
- **Type**: Gauge
- **Description**: Total number of nodes matching the label criteria
- **Use Case**: Monitor the current number of nodes that match the configured label selectors

## Shared Metrics

These metrics aggregate data across all detection methods (Karpenter and node label detection) to provide a unified view of node parking activity.

### `shredder_nodes_parked_total`
- **Type**: Counter
- **Description**: Total number of nodes successfully parked (shared across all detection methods)
- **Use Case**: Monitor total node parking activity regardless of detection method

### `shredder_nodes_parking_failed_total`
- **Type**: Counter
- **Description**: Total number of nodes that failed to be parked (shared across all detection methods)
- **Use Case**: Monitor total node parking failures regardless of detection method

### `shredder_processing_duration_seconds`
- **Type**: Summary
- **Description**: Duration of node processing in seconds (shared across all detection methods)
- **Objectives**: 0.5: 0.05, 0.9: 0.01, 0.99: 0.001
- **Use Case**: Monitor total node processing performance regardless of detection method

## Metric Relationships

### Detection Method Metrics
- **Karpenter metrics** are incremented when `EnableKarpenterDriftDetection=true`
- **Node label metrics** are incremented when `EnableNodeLabelDetection=true`
- **Shared metrics** are incremented whenever either detection method processes nodes

### Processing Flow
1. **Detection**: Nodes are identified via Karpenter drift or label matching
2. **Parking**: Nodes are labeled, cordoned, and tainted
3. **Eviction**: Pods are evicted from parked nodes over time
4. **Cleanup**: Nodes are eventually removed when all pods are evicted

## Alerting Recommendations

### High Error Rates
```promql
rate(shredder_errors_total[5m]) > 0.1
```

### Slow Processing
```promql
histogram_quantile(0.95, rate(shredder_processing_duration_seconds_bucket[5m])) > 30
```

### Failed Node Parking
```promql
rate(shredder_nodes_parking_failed_total[5m]) > 0
```

### High API Latency
```promql
histogram_quantile(0.95, rate(shredder_apiserver_requests_duration_seconds_bucket[5m])) > 5
```

### Parked Pods Alert
```promql
# Alert when pods are running on parked nodes
kube_ethos_upgrade:parked_pod > 0
```

## Example Queries

### Node Parking Success Rate
```promql
rate(shredder_nodes_parked_total[5m]) / (rate(shredder_nodes_parked_total[5m]) + rate(shredder_nodes_parking_failed_total[5m]))
```

### Average Processing Duration
```promql
histogram_quantile(0.5, rate(shredder_processing_duration_seconds_bucket[5m]))
```

### Nodes Parked by Detection Method
```promql
# Karpenter nodes
rate(shredder_karpenter_nodes_parked_total[5m])

# Label-based nodes
rate(shredder_node_label_nodes_parked_total[5m])
```

### Current Matching Nodes
```promql
shredder_node_label_matching_nodes_total
```

## Configuration

Metrics are exposed on the configured port (default: 8080) at the `/metrics` endpoint. The metrics server can be configured using the following options:

- **Metrics Port**: Configure the port for metrics exposure
- **Health Endpoint**: Available at `/healthz` for health checks
- **OpenMetrics Format**: Enabled by default for better compatibility

For more information about configuring k8s-shredder, see the [main README](../README.md). 
