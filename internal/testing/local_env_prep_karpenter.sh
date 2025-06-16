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
      --config "${test_dir}/kind-karpenter.yaml"
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

echo "KIND: setting up k8s-shredder rbac..."
kubectl apply -f "${test_dir}/rbac.yaml"

if  [[ ${ENABLE_APISERVER_DEBUG} == "true" ]]
then
  echo -e "K8S_SHREDDER: Enable debug logging on apiserver"
  TOKEN=$(kubectl create token default)

  APISERVER=$(kubectl config view -o jsonpath="{.clusters[?(@.name==\"kind-${K8S_CLUSTER_NAME}\")].cluster.server}")
curl -s -X PUT -d '5' "$APISERVER"/debug/flags/v --header "Authorization: Bearer $TOKEN" -k
fi

echo "KARPENTER: Note - this is a simplified test setup that simulates Karpenter without installing it"
echo "KARPENTER: In this test environment, we'll simulate drifted NodeClaims using mock objects"
echo "KARPENTER: The k8s-shredder Karpenter drift detection will be tested against these objects"

# Create karpenter namespace for testing
kubectl create namespace karpenter || true

# Create mock Karpenter CRDs for testing (simplified versions)
echo "KARPENTER: Creating mock Karpenter CRDs for testing..."
cat <<EOF | kubectl apply -f -
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: nodeclaims.karpenter.sh
spec:
  group: karpenter.sh
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              nodeName:
                type: string
              requirements:
                type: array
                items:
                  type: object
                  properties:
                    key:
                      type: string
                    operator:
                      type: string
                    values:
                      type: array
                      items:
                        type: string
          status:
            type: object
            properties:
              conditions:
                type: array
                items:
                  type: object
                  properties:
                    type:
                      type: string
                    status:
                      type: string
                    reason:
                      type: string
                    message:
                      type: string
              nodeName:
                type: string
              providerID:
                type: string
  scope: Cluster
  names:
    plural: nodeclaims
    singular: nodeclaim
    kind: NodeClaim
EOF

echo "KARPENTER: Mock Karpenter CRDs created for testing purposes"

echo "KARPENTER: Creating mock NodeClaims for testing k8s-shredder drift detection..."
# We'll create mock NodeClaims in the cluster upgrade script

echo "KIND: deploying k8s-shredder..."
kubectl apply -f "${test_dir}/k8s-shredder-karpenter.yaml"

echo "KIND: deploying prometheus..."
kubectl apply -f "${test_dir}/prometheus_stuffs_karpenter.yaml"

echo "KIND: deploying Argo Rollouts CRD..."
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v1.7.2/manifests/crds/rollout-crd.yaml

echo "KIND: deploying test applications..."
kubectl apply -f "${test_dir}/test_apps.yaml"

# Adjust the correct UID for the test-app-argo-rollout ownerReference
rollout_uid=$(kubectl -n ns-team-k8s-shredder-test get rollout test-app-argo-rollout -o jsonpath='{.metadata.uid}')
sed "s/REPLACE_WITH_ROLLOUT_UID/${rollout_uid}/" < "${test_dir}/test_apps.yaml"  | kubectl apply -f -

echo "KARPENTER: Mock Karpenter test environment ready!"

echo "K8S_SHREDDER: waiting for k8s-shredder deployment to become ready!"
retry_count=0
i=1
sp="/-\|"
while [[  ${status} == *"False"* || -z ${status} ]]; do
  # set 5 minute timeout
  if [[ ${retry_count} == 600 ]]; then echo "Timeout exceeded!" && exit 1; fi
  # shellcheck disable=SC2059
  printf "\b${sp:i++%${#sp}:1}" && sleep 0.5;
  status=$(kubectl get pods -n kube-system -l app=k8s-shredder -o json | \
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
kubectl logs -l app=k8s-shredder -n kube-system

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
kubectl logs -n kube-system -l app=k8s-shredder --kubeconfig=${KUBECONFIG_FILE} \n"

echo -e "K8S_SHREDDER: You can access prometheus metrics at http://localhost:1234 after running
kubectl port-forward -n kube-system svc/prometheus --kubeconfig=${KUBECONFIG_FILE} 1234:9090\n"

echo "KARPENTER: Environment setup complete!"
echo "KARPENTER: Mock Karpenter CRDs are ready for testing"
echo ""
echo "KARPENTER: To test drift detection, the upgrade script will create mock drifted NodeClaims..." 