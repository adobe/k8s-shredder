#!/bin/bash

# EvictionSafetyCheck E2E Test Script
# 
# This script runs the EvictionSafetyCheck e2e tests for k8s-shredder.
# The tests verify that k8s-shredder properly validates that all pods on a parked node 
# have the required parking labels before proceeding with force eviction.
#
# Test Cases:
# 1. TestEvictionSafetyCheck (Failure Case): Tests that nodes are unparked when pods lack proper labels
# 2. TestEvictionSafetyCheckPasses (Success Case): Tests that force eviction proceeds when all pods are properly labeled
#
# The failure test includes a PodDisruptionBudget step to prevent soft eviction of the unlabeled pod,
# ensuring the pod remains on the node when the safety check runs.
#
# Prerequisites:
# - A running kind cluster with k8s-shredder deployed
# - EvictionSafetyCheck enabled in the k8s-shredder configuration
# - The park-node binary built and available
#
# Usage:
#   ./test_eviction_safety_check.sh
#
# The tests will automatically skip if:
# - EvictionSafetyCheck is disabled in the k8s-shredder configuration
# - Running in Karpenter or node-labels test environments (different node structures)

set -e

echo "Running EvictionSafetyCheck E2E Tests..."

# Check if we're in the right directory
if [ ! -f "internal/testing/e2e_test.go" ]; then
    echo "Error: This script must be run from the k8s-shredder project root"
    exit 1
fi

# Build the park-node binary if it doesn't exist
if [ ! -f "park-node" ]; then
    echo "Building park-node binary..."
    make build
fi

# Run the e2e tests
echo "Running e2e tests..."
make e2e-tests

echo "EvictionSafetyCheck E2E Tests completed!" 
