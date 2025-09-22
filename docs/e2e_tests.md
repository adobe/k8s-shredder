# k8s-shredder End-to-End Tests

This document describes all the end-to-end tests for k8s-shredder, including their functionality, skip conditions, and execution environments.

## Test Overview

The e2e tests verify various aspects of k8s-shredder functionality including node parking, pod eviction, metrics collection, and safety checks. Tests are designed to run in different environments (standard, Karpenter, node-labels) and have specific skip conditions.

## Test Environments

### Standard Environment (`local-test`)
- **Purpose**: Basic k8s-shredder functionality testing
- **Cluster**: 4 nodes (control-plane, worker, worker2, worker3-monitoring)
- **Features**: Standard node parking and pod eviction

### Karpenter Environment (`local-test-karpenter`)
- **Purpose**: Karpenter drift detection testing
- **Cluster**: 4 nodes (control-plane, worker, worker2, worker3-monitoring)
- **Features**: Mock Karpenter CRDs, drift detection simulation

### Node Labels Environment (`local-test-node-labels`)
- **Purpose**: Node label detection testing
- **Cluster**: 5 nodes (control-plane, worker, worker2, worker3, worker4-monitoring)
- **Features**: Node label detection, automatic parking based on labels

## Test Cases

### TestNodeIsCleanedUp
**Always Run**: ✅ Yes (in all environments)

**Purpose**: Verifies that k8s-shredder properly cleans up parked nodes after their TTL expires.

**Steps**:
1. Parks a worker node with a 1-minute TTL
2. Waits for the TTL to expire
3. Verifies that all user pods are evicted from the node
4. Collects metrics to verify operation

**Expected Result**: The node should be parked, then after TTL expiration, all user pods should be evicted.

**Skip Conditions**: None - runs in all environments

---

### TestShredderMetrics
**Always Run**: ✅ Yes (in all environments)

**Purpose**: Verifies that k8s-shredder metrics are properly collected and exposed via Prometheus.

**Steps**:
1. Collects metrics from Prometheus
2. Verifies that expected metrics are present
3. Logs metric values for verification

**Expected Result**: Should find metrics like `shredder_processed_pods_total`, `shredder_errors_total`, etc.

**Skip Conditions**: None - runs in all environments

**Note**: Requires Prometheus to be running on the dedicated monitoring node (worker3/worker4)

---

### TestArgoRolloutRestartAt
**Always Run**: ✅ Yes (in all environments)

**Purpose**: Verifies that k8s-shredder properly sets the `restartAt` field on Argo Rollouts.

**Steps**:
1. Waits for the Argo Rollout to have its `restartAt` field set
2. Verifies the field is properly configured

**Expected Result**: The Argo Rollout should have a `restartAt` field set to a future timestamp.

**Skip Conditions**: None - runs in all environments

---

### TestKarpenterMetrics
**Conditional Run**: Only in Karpenter environment

**Purpose**: Verifies Karpenter-specific metrics when drift detection is enabled.

**Steps**:
1. Collects Karpenter-specific metrics from Prometheus
2. Verifies expected Karpenter metrics are present
3. Logs metric values for verification

**Expected Result**: Should find metrics like `shredder_karpenter_drifted_nodes_total`, `shredder_karpenter_nodes_parked_total`, etc.

**Skip Conditions**: 
- ❌ Not running in Karpenter test environment
- ❌ Prometheus not accessible

---

### TestNodeLabelMetrics
**Conditional Run**: Only in node-labels environment

**Purpose**: Verifies node label detection metrics when node label detection is enabled.

**Steps**:
1. Collects node label detection metrics from Prometheus
2. Verifies expected node label metrics are present
3. Logs metric values for verification

**Expected Result**: Should find metrics like `shredder_node_label_nodes_parked_total`, `shredder_node_label_matching_nodes_total`, etc.

**Skip Conditions**:
- ❌ Not running in node-labels test environment
- ❌ Prometheus not accessible

---

### TestEvictionSafetyCheck
**Conditional Run**: Only when EvictionSafetyCheck is enabled

**Purpose**: Tests the EvictionSafetyCheck failure case - verifies that nodes are unparked when pods lack proper parking labels.

**Steps**:
1. Scale k8s-shredder replicas to zero to disable actions
2. Park the worker2 node and all pods on it (properly labels all existing pods)
3. Create a new pod without proper parking labels on worker2
4. Create a PodDisruptionBudget to prevent soft eviction of the unlabeled pod
5. Scale k8s-shredder replicas to 1 to start the test
6. Monitor worker2 parking status - it should be unparked due to safety check failure

**Expected Result**: The node should be unparked because the EvictionSafetyCheck detects that not all pods have proper parking labels.

**Skip Conditions**:
- ❌ EvictionSafetyCheck is disabled in k8s-shredder configuration
- ❌ Running in Karpenter or node-labels test environments (different node structures)
- ❌ Cannot access k8s-shredder-config configmap

---

### TestEvictionSafetyCheckPasses
**Conditional Run**: Only when EvictionSafetyCheck is enabled

**Purpose**: Tests the EvictionSafetyCheck success case - verifies that force eviction proceeds when all pods are properly labeled.

**Steps**:
1. Scale k8s-shredder replicas to zero to disable actions
2. Park the worker2 node and all pods on it (this properly labels all pods)
3. Scale k8s-shredder replicas to 1 to start the test
4. Monitor worker2 parking status - it should remain parked until TTL expires, then get force evicted

**Expected Result**: The node should remain parked and eventually be force evicted because all pods have proper parking labels.

**Skip Conditions**:
- ❌ EvictionSafetyCheck is disabled in k8s-shredder configuration
- ❌ Running in Karpenter or node-labels test environments (different node structures)
- ❌ Cannot access k8s-shredder-config configmap

## Running the Tests

### Prerequisites

1. A running kind cluster with k8s-shredder deployed
2. The `park-node` binary built and available
3. Prometheus running on the dedicated monitoring node

### Running All Tests

```bash
# Build the park-node binary
make build

# Run all e2e tests
make e2e-tests
```

### Running Specific Test Environments

```bash
# Standard environment
make local-test

# Karpenter environment
make local-test-karpenter

# Node labels environment
make local-test-node-labels
```

### Running Individual Tests

```bash
# Run specific test
PROJECT_ROOT=${PWD} KUBECONFIG=${PWD}/kubeconfig-localtest go test internal/testing/e2e_test.go -v -run TestShredderMetrics

# Run all EvictionSafetyCheck tests
PROJECT_ROOT=${PWD} KUBECONFIG=${PWD}/kubeconfig-localtest go test internal/testing/e2e_test.go -v -run 'TestEvictionSafetyCheck.*'
```

## Test Configuration

### EvictionSafetyCheck Configuration

The EvictionSafetyCheck tests check the `k8s-shredder-config` ConfigMap in the `kube-system` namespace for the `EvictionSafetyCheck: true` setting. If this setting is not found or is set to `false`, the tests will be skipped.

### Prometheus Configuration

All metrics tests require Prometheus to be running on the dedicated monitoring node. The monitoring node is configured with:
- **Node Label**: `monitoring=dedicated`
- **Node Taint**: `monitoring=dedicated:NoSchedule`
- **Prometheus Node Selector**: `monitoring: dedicated`
- **Prometheus Toleration**: For the `monitoring=dedicated:NoSchedule` taint

This ensures Prometheus is never affected by k8s-shredder node parking operations.

## PodDisruptionBudget Usage

In the `TestEvictionSafetyCheck` failure test case, a PodDisruptionBudget is created to prevent the unlabeled pod from being evicted by normal "soft" eviction mechanisms before the EvictionSafetyCheck runs. This ensures that:

1. The pod remains on the node when k8s-shredder performs the safety check
2. The safety check can properly detect the missing parking labels
3. The node gets unparked as expected

The PDB uses `minAvailable: 1` and targets the specific test pod using the `test-pod: "true"` label selector.

## Test Results Interpretation

### Successful Test Results
- **PASS**: Test completed successfully with expected behavior
- **SKIP**: Test was skipped due to environment or configuration conditions

### Failed Test Results
- **FAIL**: Test failed due to unexpected behavior or errors
- **TIMEOUT**: Test exceeded maximum execution time

### Common Skip Reasons
- `EvictionSafetyCheck is disabled in k8s-shredder configuration`
- `not running in a Karpenter test environment`
- `not running in a node labels test environment`
- `Prometheus is not accessible after 30 retries`

## Troubleshooting

### Prometheus Issues
If metrics tests are failing with "Prometheus port not set" errors:
1. Check that Prometheus is running: `kubectl get pods -n kube-system | grep prometheus`
2. Verify Prometheus is on the monitoring node: `kubectl get pods -n kube-system -o wide | grep prometheus`
3. Check node labels and taints: `kubectl describe node <monitoring-node>`

### EvictionSafetyCheck Issues
If EvictionSafetyCheck tests are being skipped:
1. Check the configmap: `kubectl get configmap k8s-shredder-config -n kube-system -o yaml`
2. Verify `EvictionSafetyCheck: true` is set in the configuration
3. Ensure you're running in the standard test environment (not Karpenter or node-labels)

### Node Parking Issues
If node parking tests are failing:
1. Check k8s-shredder logs: `kubectl logs -n kube-system -l app.kubernetes.io/name=k8s-shredder`
2. Verify the park-node binary exists: `ls -la park-node`
3. Check node status: `kubectl get nodes` 
