#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1
KUBECONFIG_FILE=${2:-kubeconfig}

echo "K8S_SHREDDER: Simulating cluster upgrade..."
echo "K8S_SHREDDER: Parking k8s-shredder-worker with proper pod labeling and a TTL of 1 minute!"

# Use the park-node binary to properly park the node (labels both node and pods)
./park-node -node "${K8S_CLUSTER_NAME}-worker" -park-kubeconfig "${KUBECONFIG_FILE}"

if [[ ${WAIT_FOR_PODS:-false} == "true" ]]
then
  while [[ $pod_status != "No resources found" ]]
  do
    echo "Info: Waiting for all pods to be evicted from the node..."
    sleep 10
    pod_status=$(kubectl get pods -A --field-selector metadata.namespace!=kube-system,metadata.namespace!=local-path-storage,spec.nodeName=k8s-shredder-test-cluster-worker 2>&1 >/dev/null)
  done

  # This is to simulate the upgrade process. We are going to wait for 1 minute and then uncordon the node.
  kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=${KUBECONFIG_FILE} shredder.ethos.adobe.net/upgrade-status-
  kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=${KUBECONFIG_FILE} --overwrite shredder.ethos.adobe.net/parked-node-expires-on-
fi
