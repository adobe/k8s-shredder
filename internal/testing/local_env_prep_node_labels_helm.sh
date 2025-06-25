#!/bin/bash
set -e

K8S_SHREDDER_VERSION=$1
KINDNODE_VERSION=$2
K8S_CLUSTER_NAME=$3
KUBECONFIG_FILE=${4:-kubeconfig}

test_dir=$(dirname "${BASH_SOURCE[0]}")

if kind get clusters | grep "${K8S_CLUSTER_NAME}" ; then
  echo "Local environment should be already set up. If that is not the case run 'make clean' first";
  [[ -z "${KUBECONFIG}" ]] && export KUBECONFIG=${KUBECONFIG_FILE}
else
  # create a k8s cluster
  echo "KIND: creating cluster ${K8S_CLUSTER_NAME} with version ${KINDNODE_VERSION}..."
  kind create cluster --name "${K8S_CLUSTER_NAME}" --kubeconfig=${KUBECONFIG_FILE} --image "kindest/node:${KINDNODE_VERSION}" \
      --config "${test_dir}/kind-node-labels.yaml"
  export KUBECONFIG=${KUBECONFIG_FILE}
fi

# upload k8s-shredder image inside kind cluster
kind load docker-image adobe/k8s-shredder:"${K8S_SHREDDER_VERSION}" --name "${K8S_CLUSTER_NAME}"

namespace_status=$(kubectl get ns ns-k8s-shredder-test -o json | jq .status.phase -r)

if [[ $namespace_status == "Active" ]]
then
    echo "KIND: Namespace ns-k8s-shredder-test and ns-team-k8s-shredder-test already present"
else
  echo "KIND: creating ns-team-k8s-shredder-test and ns-k8s-shredder-test namespaces..."
  kubectl create namespace ns-k8s-shredder-test
  kubectl create namespace ns-team-k8s-shredder-test
fi

if  [[ ${ENABLE_APISERVER_DEBUG} == "true" ]]
then
  echo -e "K8S_SHREDDER: Enable debug logging on apiserver"
  TOKEN=$(kubectl create token default)

  APISERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"kind-${K8S_CLUSTER_NAME}\")].cluster.server}")
curl -s -X PUT -d '5' "$APISERVER"/debug/flags/v --header "Authorization: Bearer $TOKEN" -k
fi

echo "NODE_LABELS: This test environment will demonstrate node label detection functionality"
echo "NODE_LABELS: k8s-shredder will detect nodes with specific labels and park them"

echo "KIND: deploying k8s-shredder using Helm chart with node label detection enabled..."
# Use Helm to deploy k8s-shredder with node label detection enabled
helm install k8s-shredder "${test_dir}/../../charts/k8s-shredder" \
  --namespace kube-system \
  --set image.registry=adobe/k8s-shredder \
  --set image.tag="${K8S_SHREDDER_VERSION}" \
  --set image.pullPolicy=Never \
  --set shredder.EvictionLoopInterval=30s \
  --set shredder.ParkedNodeTTL=2m \
  --set shredder.RollingRestartThreshold=0.5 \
  --set shredder.EnableKarpenterDriftDetection=false \
  --set shredder.EnableNodeLabelDetection=true \
  --set shredder.NodeLabelsToDetect[0]="test-label" \
  --set shredder.NodeLabelsToDetect[1]="maintenance=scheduled" \
  --set shredder.NodeLabelsToDetect[2]="node.test.io/park" \
  --set logLevel=info \
  --set logFormat=text \
  --set dryRun=false \
  --set service.create=true \
  --set service.type=ClusterIP \
  --set service.port=8080 \
  --set service.targetPort=metrics

echo "KIND: deploying prometheus..."
kubectl apply -f "${test_dir}/prometheus_stuffs_node_labels.yaml"

echo "KIND: deploying Argo Rollouts CRD..."
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v1.7.2/manifests/crds/rollout-crd.yaml

echo "KIND: deploying test applications..."
kubectl apply -f "${test_dir}/test_apps.yaml"

# Adjust the correct UID for the test-app-argo-rollout ownerReference
rollout_uid=$(kubectl -n ns-team-k8s-shredder-test get rollout test-app-argo-rollout -o jsonpath='{.metadata.uid}')
sed "s/REPLACE_WITH_ROLLOUT_UID/${rollout_uid}/" < "${test_dir}/test_apps.yaml"  | kubectl apply -f -

echo "NODE_LABELS: Node label detection test environment ready!"

echo "K8S_SHREDDER: waiting for k8s-shredder deployment to become ready!"
retry_count=0
i=1
sp="/-\|"
while [[  ${status} == *"False"* || -z ${status} ]]; do
  # set 5 minute timeout
  if [[ ${retry_count} == 600 ]]; then echo "Timeout exceeded!" && exit 1; fi
  # shellcheck disable=SC2059
  printf "\b${sp:i++%${#sp}:1}" && sleep 0.5;
  status=$(kubectl get pods -n kube-system -l app.kubernetes.io/name=k8s-shredder -o json | \
        jq '.items[].status.conditions[] | select(.type=="Ready")| .status' 2> /dev/null)
  retry_count=$((retry_count+1))
done
echo ""

echo "K8S_SHREDDER: waiting for rollout object PDB to become ready!"
retry_count=0
while [[ $(kubectl get pdb -n ns-team-k8s-shredder-test test-app-argo-rollout \
           -o jsonpath="{.status.currentHealthy}"  2> /dev/null) != "2" ]]; do
  # set 5 minute timeout
  if [[ ${retry_count} == 600 ]]; then echo "Timeout exceeded!" && exit 1; fi
  # shellcheck disable=SC2059
  printf "\b${sp:i++%${#sp}:1}" && sleep 0.5;
  retry_count=$((retry_count+1))
done

echo ""
kubectl logs -l app.kubernetes.io/name=k8s-shredder -n kube-system

echo "K8S_SHREDDER: waiting for prometheus deployment to become ready!"
retry_count=0
while [[ $(kubectl get pods -n kube-system -l app=prometheus \
           -o jsonpath="{.items[0].status.conditions[?(@.type=='Ready')].status}"  2> /dev/null) != "True" ]]; do
  # set 5 minute timeout
  if [[ ${retry_count} == 600 ]]; then echo "Timeout exceeded!" && exit 1; fi
  # shellcheck disable=SC2059
  printf "\b${sp:i++%${#sp}:1}" && sleep 0.5;
  retry_count=$((retry_count+1))
done

echo ""

echo -e "K8S_SHREDDER: You can access k8s-shredder metrics at http://localhost:1234/metrics after running
kubectl port-forward -n kube-system svc/k8s-shredder --kubeconfig=${KUBECONFIG_FILE} 1234:8080\n
It can take few minutes before seeing k8s-shredder metrics..."

echo -e "K8S_SHREDDER: You can access k8s-shredder logs by running
kubectl logs -n kube-system -l app.kubernetes.io/name=k8s-shredder --kubeconfig=${KUBECONFIG_FILE} \n"

echo -e "K8S_SHREDDER: You can access prometheus metrics at http://localhost:1234 after running
kubectl port-forward -n kube-system svc/prometheus --kubeconfig=${KUBECONFIG_FILE} 1234:9090\n"

echo "NODE_LABELS: Environment setup complete!"
echo "NODE_LABELS: Configured to detect nodes with these labels:"
echo "  - test-label (key only)"
echo "  - maintenance=scheduled (key=value)"
echo "  - node.test.io/park (key only)"
echo ""

echo "NODE_LABELS: Now applying test labels to trigger node label detection..."

# Apply test labels to trigger k8s-shredder's node label detection
WORKER_NODES=($(kubectl get nodes --no-headers -o custom-columns=NAME:.metadata.name | grep -v control-plane))
WORKER_NODE1=${WORKER_NODES[0]}
WORKER_NODE2=${WORKER_NODES[1]}

echo "NODE_LABELS: Adding 'test-label=test-value' to node ${WORKER_NODE1}"
kubectl label node "${WORKER_NODE1}" test-label=test-value

echo "NODE_LABELS: Adding 'maintenance=scheduled' to node ${WORKER_NODE2}"  
kubectl label node "${WORKER_NODE2}" maintenance=scheduled

echo "NODE_LABELS: Labels applied! k8s-shredder should detect and park these nodes shortly..."
echo "NODE_LABELS: You can monitor the process with:"
echo "  kubectl logs -n kube-system -l app.kubernetes.io/name=k8s-shredder --kubeconfig=${KUBECONFIG_FILE} -f" 
