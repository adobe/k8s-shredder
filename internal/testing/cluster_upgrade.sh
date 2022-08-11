#!/bin/bash
set -e

K8S_CLUSTER_NAME=$1

echo "K8S_SHREDDER: Simulating cluster upgrade..."
echo "K8S_SHREDDER: Cordoning and labelling k8s-shredder-worker as parked with a TTL of 1 minute!"
kubectl cordon "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status=parked --overwrite=true
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on="$(date -v '+1M' -u +'%s')"

# For moving node back as active, useful during debug processs
#export K8S_CLUSTER_NAME=k8s-shredder-test-cluster
#kubectl uncordon "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig
#kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status-
#kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on-
#kubectl delete -n ns-k8s-shredder-test $(kubectl get pods -n ns-k8s-shredder-test -oname) --force --wait=0 --timeout=0
#kubectl delete -n ns-team-k8s-shredder-test $(kubectl get pods -n ns-team-k8s-shredder-test -oname) --force --wait=0 --timeout=0
#kubectl get po -A --field-selector=spec.nodeName=k8s-shredder-test-cluster-worker
