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
  EXPIRES_ON=$(date -d '+1 minutes' +"%s")
else
  EXPIRES_ON=$(date -v '+1M' -u +'%s')
fi

kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on="${EXPIRES_ON}"

# For moving node back as active, useful during debug process
#export K8S_CLUSTER_NAME=k8s-shredder-test-cluster
#kubectl uncordon "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig
#kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status-
#kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on-
#kubectl delete -n ns-k8s-shredder-test $(kubectl get pods -n ns-k8s-shredder-test -oname) --force --wait=0 --timeout=0
#kubectl delete -n ns-team-k8s-shredder-test $(kubectl get pods -n ns-team-k8s-shredder-test -oname) --force --wait=0 --timeout=0
#kubectl get po -A --field-selector=spec.nodeName=k8s-shredder-test-cluster-worker
