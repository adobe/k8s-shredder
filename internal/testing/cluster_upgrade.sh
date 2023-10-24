#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1

echo "K8S_SHREDDER: Simulating cluster upgrade..."
echo "K8S_SHREDDER: Cordoning and labelling k8s-shredder-worker as parked with a TTL of 1 minute!"
kubectl cordon "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status=parked --overwrite=true

# date is not a bash builtin. It is a system utility and that is something on which OSX and Linux differ.
# OSX uses BSD tools while Linux uses GNU tools. They are similar but not the same.
if [[ "$(uname)" = Linux ]]
then
  EXPIRES_ON=$(date -d '+1 minutes' +"%s".000)
else
  EXPIRES_ON=$(date -v '+1M' -u +'%s'.000)
fi

kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on="${EXPIRES_ON}"

while [[ $pod_status != "No resources found" ]]
do
  echo "Info: Waiting for all pods to be evicted from the node..."
  sleep 10
  pod_status=$(kubectl get pods -A --field-selector metadata.namespace!=kube-system,metadata.namespace!=local-path-storage,spec.nodeName=k8s-shredder-test-cluster-worker 2>&1 >/dev/null)
done

# This is to simulate the upgrade process. We are going to wait for 1 minute and then uncordon the node.
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status-
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on-


