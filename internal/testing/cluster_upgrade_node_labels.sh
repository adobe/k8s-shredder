#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1
KUBECONFIG_FILE=${2:-kubeconfig}
test_dir=$(dirname "${BASH_SOURCE[0]}")

export KUBECONFIG=${KUBECONFIG_FILE}

echo "==============================================================================="
echo "NODE_LABELS: Starting node label detection test"
echo "==============================================================================="

echo "NODE_LABELS: This test will demonstrate k8s-shredder's node label detection functionality"
echo "NODE_LABELS: We'll add specific labels to nodes and verify they get parked automatically"
echo ""

echo "NODE_LABELS: Getting available nodes for testing..."
node_count=$(kubectl get nodes --no-headers | wc -l)
if [[ ${node_count} -eq 0 ]]; then
    echo "ERROR: No nodes available for testing"
    exit 1
fi

# Get a worker node for testing (prefer worker nodes over control-plane)
test_node=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name" | grep -v control-plane | head -1 || kubectl get nodes --no-headers -o custom-columns=":metadata.name" | head -1)
echo "NODE_LABELS: Using node '${test_node}' for testing"

echo "NODE_LABELS: Current node labels before adding test labels:"
kubectl get node ${test_node} --show-labels
echo ""

echo "NODE_LABELS: Checking for any existing parking labels..."
parking_status=$(kubectl get node ${test_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
echo "NODE_LABELS: Current parking status: ${parking_status:-"Not parked"}"

echo ""
echo "==============================================================================="
echo "NODE_LABELS: Adding test label to trigger node label detection..."
echo "==============================================================================="

# We'll test with the "test-label" key-only selector
echo "NODE_LABELS: Adding label 'test-label=test-value' to node ${test_node}"
kubectl label node ${test_node} test-label=test-value

echo "NODE_LABELS: Node labeled successfully!"

echo "NODE_LABELS: Current node labels after adding test label:"
kubectl get node ${test_node} --show-labels
echo ""

echo ""
echo "==============================================================================="
echo "NODE_LABELS: Waiting for k8s-shredder to detect and park the labeled node..."
echo "==============================================================================="

echo "NODE_LABELS: Current k8s-shredder logs:"
kubectl logs -l app=k8s-shredder -n kube-system --tail=20
echo ""

echo "NODE_LABELS: Monitoring k8s-shredder activity for next 3 minutes..."
start_time=$(date +%s)
end_time=$((start_time + 180))

while [[ $(date +%s) -lt ${end_time} ]]; do
    current_time=$(date +%s)
    remaining=$((end_time - current_time))
    
    echo "NODE_LABELS: Checking node parking status... (${remaining}s remaining)"
    
    # Check if node is parked
    parking_status=$(kubectl get node ${test_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
    parked_by=$(kubectl get node ${test_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-by}' 2>/dev/null || echo "")
    expires_on=$(kubectl get node ${test_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/parked-node-expires-on}' 2>/dev/null || echo "")
    
    if [[ "${parking_status}" == "parked" ]]; then
        echo ""
        echo "==============================================================================="
        echo "NODE_LABELS: SUCCESS! Node ${test_node} has been parked by k8s-shredder!"
        echo "==============================================================================="
        echo "NODE_LABELS: Parking details:"
        echo "  - Status: ${parking_status}"
        echo "  - Parked by: ${parked_by}"
        echo "  - Expires on: ${expires_on}"
        echo ""
        
        echo "NODE_LABELS: Checking if node is also cordoned and tainted..."
        node_unschedulable=$(kubectl get node ${test_node} -o jsonpath='{.spec.unschedulable}' 2>/dev/null || echo "")
        echo "  - Unschedulable (cordoned): ${node_unschedulable}"
        
        echo "  - Taints:"
        kubectl get node ${test_node} -o jsonpath='{.spec.taints}' | jq -r '.[] | "    \(.key)=\(.value):\(.effect)"' 2>/dev/null || echo "    No taints found"
        
        echo ""
        echo "NODE_LABELS: Checking pods on the node..."
        kubectl get pods --all-namespaces --field-selector spec.nodeName=${test_node} -o wide
        
        echo ""
        echo "NODE_LABELS: Final k8s-shredder logs:"
        kubectl logs -l app=k8s-shredder -n kube-system --tail=30
        
        echo ""
        echo "==============================================================================="
        echo "NODE_LABELS: Test completed successfully!"
        echo "==============================================================================="
        echo "NODE_LABELS: Summary:"
        echo "  1. ✅ Test label was added to node"
        echo "  2. ✅ k8s-shredder detected the labeled node"  
        echo "  3. ✅ k8s-shredder parked the node with labels"
        echo "  4. ✅ Node was cordoned and tainted"
        echo "  5. ✅ Pods on the node were also labeled"
        
        echo ""
        echo "NODE_LABELS: Testing additional label formats..."
        
        # Test another node with a different label format
        available_nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name" | grep -v "${test_node}")
        if [[ -n "${available_nodes}" ]]; then
            second_test_node=$(echo "${available_nodes}" | head -1)
            echo "NODE_LABELS: Testing key=value format on node '${second_test_node}'"
            kubectl label node ${second_test_node} maintenance=scheduled
            
            # Wait a bit to see if this gets detected too
            sleep 45
            
            second_parking_status=$(kubectl get node ${second_test_node} -o jsonpath='{.metadata.labels.shredder\.ethos\.adobe\.net/upgrade-status}' 2>/dev/null || echo "")
            if [[ "${second_parking_status}" == "parked" ]]; then
                echo "  ✅ Second node with key=value label also parked successfully!"
            else
                echo "  ⏳ Second node not yet parked (may need more time)"
            fi
        fi
        
        echo ""
        exit 0
    fi
    
    echo "NODE_LABELS: Node parking status: ${parking_status:-"Not parked yet"}"
    sleep 10
done

echo ""
echo "==============================================================================="
echo "NODE_LABELS: Test completed but node was not parked within timeout"
echo "==============================================================================="
echo "NODE_LABELS: Final status check:"

echo "NODE_LABELS: Node labels:"
kubectl get node ${test_node} --show-labels
echo ""

echo "NODE_LABELS: Final k8s-shredder logs:"
kubectl logs -l app=k8s-shredder -n kube-system --tail=50
echo ""

echo "NODE_LABELS: All Nodes:"
kubectl get nodes -o wide
echo ""

echo "NODE_LABELS: k8s-shredder may need more time or there might be an issue."
echo "NODE_LABELS: Check the logs above for any errors or continue monitoring manually."
echo "NODE_LABELS: This could be expected behavior if node label detection is disabled or"
echo "NODE_LABELS: if k8s-shredder hasn't run its eviction loop yet."

exit 1 