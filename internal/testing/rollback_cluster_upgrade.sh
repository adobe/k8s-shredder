#!/usr/bin/env bash
set -e

K8S_CLUSTER_NAME=$1

# For moving node back as active, useful during debug process
export K8S_CLUSTER_NAME=k8s-shredder-test-cluster
kubectl uncordon "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig shredder.ethos.adobe.net/upgrade-status-
kubectl label node "${K8S_CLUSTER_NAME}-worker" --kubeconfig=kubeconfig --overwrite shredder.ethos.adobe.net/parked-node-expires-on-
kubectl delete -n ns-k8s-shredder-test $(kubectl get pods -n ns-k8s-shredder-test -oname) --force --wait=0 --timeout=0
kubectl delete -n ns-team-k8s-shredder-test $(kubectl get pods -n ns-team-k8s-shredder-test -oname) --force --wait=0 --timeout=0
kubectl get po -A --field-selector=spec.nodeName=k8s-shredder-test-cluster-worker