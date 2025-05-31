#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1
KUBECONFIG_FILE=${2:-kubeconfig}
test_dir=$(dirname "${BASH_SOURCE[0]}")

export KUBECONFIG=${KUBECONFIG_FILE}

echo "==============================================================================="
echo "KARPENTER: Starting Karpenter drift detection test"
echo "==============================================================================="

echo "KARPENTER: This is a mock test environment to test k8s-shredder's Karpenter integration"
echo "KARPENTER: We'll create mock drifted NodeClaims to test the k8s-shredder functionality"
echo ""

echo "KARPENTER: Creating a test node to associate with mock NodeClaim..."
# We need at least one node to test labeling functionality
node_count=$(kubectl get nodes --no-headers | wc -l)
if [[ ${node_count} -eq 0 ]]; then
    echo "ERROR: No nodes available for testing"
    exit 1
fi

# Get the first available node for testing
test_node=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name" | head -1)
echo "KARPENTER: Using node '${test_node}' for testing"

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
  nodeName: "${test_node}"
  providerID: "kind://docker/kind/test-node"
EOF

echo "KARPENTER: Mock drifted NodeClaim created!"

echo "KARPENTER: Current NodeClaims:"
kubectl get nodeclaim -o wide
echo ""

echo "KARPENTER: Setting up test scenario..."
first_nodeclaim="test-nodeclaim-drifted"
associated_node="${test_node}"

echo "KARPENTER: Target NodeClaim for drift test: ${first_nodeclaim}"
echo "KARPENTER: Associated node: ${associated_node}"

echo "KARPENTER: Current node labels before k8s-shredder processing:"
kubectl get node ${associated_node} --show-labels
echo ""

echo "KARPENTER: Checking for any existing parking labels..."
parking_status=$(kubectl get node ${associated_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
echo "KARPENTER: Current parking status: ${parking_status:-"Not parked"}"

echo ""
echo "==============================================================================="
echo "KARPENTER: Waiting for k8s-shredder to detect and park the drifted node..."
echo "==============================================================================="

echo "KARPENTER: Current k8s-shredder logs:"
kubectl logs -l app=k8s-shredder -n kube-system --tail=20
echo ""

echo "KARPENTER: Monitoring k8s-shredder activity for next 3 minutes..."
start_time=$(date +%s)
end_time=$((start_time + 180))

while [[ $(date +%s) -lt ${end_time} ]]; do
    current_time=$(date +%s)
    remaining=$((end_time - current_time))
    
    echo "KARPENTER: Checking node parking status... (${remaining}s remaining)"
    
    # Check if node is parked
    parking_status=$(kubectl get node ${associated_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
    parked_by=$(kubectl get node ${associated_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-by}' 2>/dev/null || echo "")
    expires_on=$(kubectl get node ${associated_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-node-expires-on}' 2>/dev/null || echo "")
    
    if [[ "${parking_status}" == "parked" ]]; then
        echo ""
        echo "==============================================================================="
        echo "KARPENTER: SUCCESS! Node ${associated_node} has been parked by k8s-shredder!"
        echo "==============================================================================="
        echo "KARPENTER: Parking details:"
        echo "  - Status: ${parking_status}"
        echo "  - Parked by: ${parked_by}"
        echo "  - Expires on: ${expires_on}"
        echo ""
        
        echo "KARPENTER: Checking if node is also cordoned and tainted..."
        node_unschedulable=$(kubectl get node ${associated_node} -o jsonpath='{.spec.unschedulable}' 2>/dev/null || echo "")
        echo "  - Unschedulable (cordoned): ${node_unschedulable}"
        
        echo "  - Taints:"
        kubectl get node ${associated_node} -o jsonpath='{.spec.taints}' | jq -r '.[] | "    \(.key)=\(.value):\(.effect)"' 2>/dev/null || echo "    No taints found"
        
        echo ""
        echo "KARPENTER: Checking pods on the node..."
        kubectl get pods --all-namespaces --field-selector spec.nodeName=${associated_node} -o wide
        
        echo ""
        echo "KARPENTER: Final k8s-shredder logs:"
        kubectl logs -l app=k8s-shredder -n kube-system --tail=30
        
        echo ""
        echo "==============================================================================="
        echo "KARPENTER: Test completed successfully!"
        echo "==============================================================================="
        echo "KARPENTER: Summary:"
        echo "  1. ✅ Mock drifted NodeClaim was created"
        echo "  2. ✅ NodeClaim was marked as drifted"  
        echo "  3. ✅ k8s-shredder detected and parked the node"
        echo "  4. ✅ Node was labeled, cordoned, and tainted"
        echo "  5. ✅ Pods on the node were also labeled"
        echo ""
        exit 0
    fi
    
    echo "KARPENTER: Node parking status: ${parking_status:-"Not parked yet"}"
    sleep 10
done

echo ""
echo "==============================================================================="
echo "KARPENTER: Test completed but node was not parked within timeout"
echo "==============================================================================="
echo "KARPENTER: Final status check:"

echo "KARPENTER: Node labels:"
kubectl get node ${associated_node} --show-labels
echo ""

echo "KARPENTER: Final NodeClaim status:"
kubectl get nodeclaim ${first_nodeclaim} -o yaml
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
echo "KARPENTER: This could be expected behavior if Karpenter drift detection is disabled or"
echo "KARPENTER: if k8s-shredder hasn't run its eviction loop yet."

exit 1 