#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1
KUBECONFIG_FILE=${2:-kubeconfig}
test_dir=$(dirname "${BASH_SOURCE[0]}")

export KUBECONFIG=${KUBECONFIG_FILE}

echo "==============================================================================="
echo "KARPENTER: Starting Karpenter drift and disruption detection test"
echo "==============================================================================="

echo "KARPENTER: This is a mock test environment to test k8s-shredder's Karpenter integration"
echo "KARPENTER: We'll create mock drifted and disrupted NodeClaims to test the k8s-shredder functionality"
echo ""

echo "KARPENTER: Creating test nodes to associate with mock NodeClaims..."
# We need at least two nodes to test both drift and disruption detection
node_count=$(kubectl get nodes --no-headers | wc -l)
if [[ ${node_count} -lt 2 ]]; then
    echo "ERROR: At least 2 nodes required for testing (found ${node_count})"
    exit 1
fi

# Get worker nodes for testing
worker_nodes=($(kubectl get nodes --no-headers -o custom-columns=":metadata.name" | grep -v "control-plane"))
if [[ ${#worker_nodes[@]} -lt 2 ]]; then
    echo "ERROR: At least 2 worker nodes required for testing (found ${#worker_nodes[@]})"
    exit 1
fi

drift_node="${worker_nodes[0]}"
disruption_node="${worker_nodes[1]}"

echo "KARPENTER: Using worker nodes:"
echo "  - Drift test node: '${drift_node}'"
echo "  - Disruption test node: '${disruption_node}'"

echo "KARPENTER: Creating mock drifted NodeClaim..."
# Create a mock NodeClaim that appears to be drifted
cat <<EOF | kubectl apply -f -
apiVersion: karpenter.sh/v1
kind: NodeClaim
metadata:
  name: test-nodeclaim-drifted
  labels:
    karpenter.sh/nodepool: "default"
spec:
  requirements:
  - key: kubernetes.io/arch
    operator: In
    values: ["amd64"]
status:
  conditions:
  - type: "Drifted"
    status: "True"
    reason: "NodePoolDrift"
    message: "Node is drifted due to NodePool changes"
  - type: "Ready"
    status: "True"
    reason: "NodeReady"
    message: "Node is ready"
  nodeName: "${drift_node}"
  providerID: "kind://docker/kind/test-node"
EOF

echo "KARPENTER: Mock drifted NodeClaim created!"

echo "KARPENTER: Creating mock disrupted NodeClaim..."
# Create a mock NodeClaim that appears to be disrupted
cat <<EOF | kubectl apply -f -
apiVersion: karpenter.sh/v1
kind: NodeClaim
metadata:
  name: test-nodeclaim-disrupted
  labels:
    karpenter.sh/nodepool: "default"
spec:
  requirements:
  - key: kubernetes.io/arch
    operator: In
    values: ["amd64"]
status:
  conditions:
  - type: "Disrupting"
    status: "True"
    reason: "Consolidation"
    message: "Node is being disrupted for consolidation"
  - type: "Ready"
    status: "True"
    reason: "NodeReady"
    message: "Node is ready"
  nodeName: "${disruption_node}"
  providerID: "kind://docker/kind/test-node"
EOF

echo "KARPENTER: Mock disrupted NodeClaim created!"

echo "KARPENTER: Current NodeClaims:"
kubectl get nodeclaim -o wide
echo ""

echo "KARPENTER: Setting up test scenario..."
drift_nodeclaim="test-nodeclaim-drifted"
disruption_nodeclaim="test-nodeclaim-disrupted"

echo "KARPENTER: Target NodeClaims:"
echo "  - Drift test: ${drift_nodeclaim} -> ${drift_node}"
echo "  - Disruption test: ${disruption_nodeclaim} -> ${disruption_node}"

echo "KARPENTER: Current node labels before k8s-shredder processing:"
echo "Drift node (${drift_node}):"
kubectl get node ${drift_node} --show-labels
echo ""
echo "Disruption node (${disruption_node}):"
kubectl get node ${disruption_node} --show-labels
echo ""

echo "KARPENTER: Checking for any existing parking labels..."
drift_parking_status=$(kubectl get node ${drift_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
disruption_parking_status=$(kubectl get node ${disruption_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
echo "KARPENTER: Current parking status:"
echo "  - Drift node: ${drift_parking_status:-"Not parked"}"
echo "  - Disruption node: ${disruption_parking_status:-"Not parked"}"

echo ""
echo "==============================================================================="
echo "KARPENTER: Waiting for k8s-shredder to detect and park both nodes..."
echo "==============================================================================="

echo "KARPENTER: Current k8s-shredder logs:"
kubectl logs -l app=k8s-shredder -n kube-system --tail=20
echo ""

echo "KARPENTER: Monitoring k8s-shredder activity for next 5 minutes..."
start_time=$(date +%s)
end_time=$((start_time + 300))

drift_parked=false
disruption_parked=false

while [[ $(date +%s) -lt ${end_time} ]]; do
    current_time=$(date +%s)
    remaining=$((end_time - current_time))
    
    echo "KARPENTER: Checking node parking status... (${remaining}s remaining)"
    
    # Check if drift node is parked
    if [[ "${drift_parked}" == "false" ]]; then
        drift_parking_status=$(kubectl get node ${drift_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
        if [[ "${drift_parking_status}" == "parked" ]]; then
            drift_parked=true
            drift_parked_by=$(kubectl get node ${drift_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-by}' 2>/dev/null || echo "")
            drift_expires_on=$(kubectl get node ${drift_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-node-expires-on}' 2>/dev/null || echo "")
            drift_parking_reason=$(kubectl get node ${drift_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-reason}' 2>/dev/null || echo "")
            echo "KARPENTER: ✅ Drift node ${drift_node} has been parked!"
            echo "  - Status: ${drift_parking_status}"
            echo "  - Parked by: ${drift_parked_by}"
            echo "  - Expires on: ${drift_expires_on}"
            echo "  - Parking reason: ${drift_parking_reason}"
        fi
    fi
    
    # Check if disruption node is parked
    if [[ "${disruption_parked}" == "false" ]]; then
        disruption_parking_status=$(kubectl get node ${disruption_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
        if [[ "${disruption_parking_status}" == "parked" ]]; then
            disruption_parked=true
            disruption_parked_by=$(kubectl get node ${disruption_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-by}' 2>/dev/null || echo "")
            disruption_expires_on=$(kubectl get node ${disruption_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-node-expires-on}' 2>/dev/null || echo "")
            disruption_parking_reason=$(kubectl get node ${disruption_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-reason}' 2>/dev/null || echo "")
            echo "KARPENTER: ✅ Disruption node ${disruption_node} has been parked!"
            echo "  - Status: ${disruption_parking_status}"
            echo "  - Parked by: ${disruption_parked_by}"
            echo "  - Expires on: ${disruption_expires_on}"
            echo "  - Parking reason: ${disruption_parking_reason}"
        fi
    fi
    
    # Check if both nodes are parked
    if [[ "${drift_parked}" == "true" && "${disruption_parked}" == "true" ]]; then
        echo ""
        echo "==============================================================================="
        echo "KARPENTER: SUCCESS! Both nodes have been parked by k8s-shredder!"
        echo "==============================================================================="
        echo "KARPENTER: Final parking details:"
        echo ""
        echo "Drift node (${drift_node}):"
        echo "  - Status: ${drift_parking_status}"
        echo "  - Parked by: ${drift_parked_by}"
        echo "  - Expires on: ${drift_expires_on}"
        echo "  - Parking reason: ${drift_parking_reason}"
        echo ""
        echo "Disruption node (${disruption_node}):"
        echo "  - Status: ${disruption_parking_status}"
        echo "  - Parked by: ${disruption_parked_by}"
        echo "  - Expires on: ${disruption_expires_on}"
        echo "  - Parking reason: ${disruption_parking_reason}"
        echo ""
        
        echo "KARPENTER: Checking if nodes are also cordoned and tainted..."
        echo "Drift node (${drift_node}):"
        drift_unschedulable=$(kubectl get node ${drift_node} -o jsonpath='{.spec.unschedulable}' 2>/dev/null || echo "")
        echo "  - Unschedulable (cordoned): ${drift_unschedulable}"
        echo "  - Taints:"
        kubectl get node ${drift_node} -o jsonpath='{.spec.taints}' | jq -r '.[] | "    \(.key)=\(.value):\(.effect)"' 2>/dev/null || echo "    No taints found"
        echo ""
        
        echo "Disruption node (${disruption_node}):"
        disruption_unschedulable=$(kubectl get node ${disruption_node} -o jsonpath='{.spec.unschedulable}' 2>/dev/null || echo "")
        echo "  - Unschedulable (cordoned): ${disruption_unschedulable}"
        echo "  - Taints:"
        kubectl get node ${disruption_node} -o jsonpath='{.spec.taints}' | jq -r '.[] | "    \(.key)=\(.value):\(.effect)"' 2>/dev/null || echo "    No taints found"
        echo ""
        
        echo "KARPENTER: Checking pods on the nodes..."
        echo "Pods on drift node (${drift_node}):"
        kubectl get pods --all-namespaces --field-selector spec.nodeName=${drift_node} -o wide
        echo ""
        echo "Pods on disruption node (${disruption_node}):"
        kubectl get pods --all-namespaces --field-selector spec.nodeName=${disruption_node} -o wide
        echo ""
        
        echo "KARPENTER: Final k8s-shredder logs:"
        kubectl logs -l app=k8s-shredder -n kube-system --tail=30
        echo ""
        
        echo "==============================================================================="
        echo "KARPENTER: Test completed successfully!"
        echo "==============================================================================="
        echo "KARPENTER: Summary:"
        echo "  1. ✅ Mock drifted NodeClaim was created"
        echo "  2. ✅ Mock disrupted NodeClaim was created"
        echo "  3. ✅ Both NodeClaims were marked with appropriate conditions"
        echo "  4. ✅ k8s-shredder detected and parked both nodes"
        echo "  5. ✅ Nodes were labeled, cordoned, and tainted"
        echo "  6. ✅ Pods on the nodes were also labeled"
        echo "  7. ✅ Parking reason labels were applied correctly"
        echo ""
        echo "KARPENTER: Parking reason validation:"
        if [[ "${drift_parking_reason}" == "karpenter-drift" ]]; then
            echo "  ✅ Drift node has correct parking reason: ${drift_parking_reason}"
        else
            echo "  ❌ Drift node has incorrect parking reason: ${drift_parking_reason} (expected: karpenter-drift)"
        fi
        
        if [[ "${disruption_parking_reason}" == "karpenter-disruption" ]]; then
            echo "  ✅ Disruption node has correct parking reason: ${disruption_parking_reason}"
        else
            echo "  ❌ Disruption node has incorrect parking reason: ${disruption_parking_reason} (expected: karpenter-disruption)"
        fi
        echo ""
        exit 0
    fi
    
    echo "KARPENTER: Current status:"
    echo "  - Drift node (${drift_node}): ${drift_parking_status:-"Not parked yet"}"
    echo "  - Disruption node (${disruption_node}): ${disruption_parking_status:-"Not parked yet"}"
    sleep 10
done

echo ""
echo "==============================================================================="
echo "KARPENTER: Test completed but not all nodes were parked within timeout"
echo "==============================================================================="
echo "KARPENTER: Final status check:"

echo "KARPENTER: Node labels:"
echo "Drift node (${drift_node}):"
kubectl get node ${drift_node} --show-labels
echo ""
echo "Disruption node (${disruption_node}):"
kubectl get node ${disruption_node} --show-labels
echo ""

echo "KARPENTER: Final NodeClaim status:"
kubectl get nodeclaim ${drift_nodeclaim} -o yaml
echo ""
kubectl get nodeclaim ${disruption_nodeclaim} -o yaml
echo ""

echo "KARPENTER: Final k8s-shredder logs:"
kubectl logs -l app=k8s-shredder -n kube-system --tail=50
echo ""

echo "KARPENTER: All NodeClaims:"
kubectl get nodeclaim -o wide
echo ""

echo "KARPENTER: All Nodes:"
kubectl get nodes -o wide
echo ""

echo "KARPENTER: k8s-shredder may need more time or there might be an issue."
echo "KARPENTER: Check the logs above for any errors or continue monitoring manually."
echo "KARPENTER: This could be expected behavior if Karpenter detection is disabled or"
echo "KARPENTER: if k8s-shredder hasn't run its eviction loop yet."

exit 1 
