# k8s-shredder - a novel way of dealing with kubernetes nodes blocked from draining

[![tests](https://github.com/adobe/k8s-shredder/actions/workflows/ci.yaml/badge.svg)](https://github.com/adobe/k8s-shredder/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/adobe/k8s-shredder)](https://goreportcard.com/report/github.com/adobe/k8s-shredder)
[![GoDoc](https://pkg.go.dev/badge/github.com/adobe/k8s-shredder?status.svg)](https://pkg.go.dev/github.com/adobe/k8s-shredder?tab=doc)
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/adobe/k8s-shredder)
![GitHub release (latest by date)](https://img.shields.io/github/v/release/adobe/k8s-shredder)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

<p align="center">
  <img src="docs/shredder_firefly.png" alt="K8s-Shredder project">
</p>

A common issue that kubernetes operators run into is how to balance the requirements of long-running workloads with the need to periodically cycle through worker nodes (e.g. for a kubernetes upgrade or configuration change).  Stateful set workloads (kafka, zookeeper, 
rabbitmq, redis, etc) may be sensitive to rescheduling, or application developers may simply deploy applications with pod disruption budgets that do not allow pod eviction at all.  k8s-shredder is a tool that allows the operator to automatically try to soft-evict and reschedule workloads on "parked nodes" over a set period of time before hard-deleting pods.  It does so while exposing metrics that application stewards can monitor and take action, allowing them to take whatever action is needed to reschedule their application off the node slated for replacement.

## What are "parked nodes"?

You can find more about node parking [here](docs/node-parking.md).

## Advantages of parking and shredding nodes

- allow teams running stateful apps to move their workloads off of parked nodes at their will, independent of node lifecycle
- optimizes cloud costs by dynamically purging unschedulable workers nodes (parked nodes).
- notifies (via metrics) clients that they are running workloads on parked nodes so that they can take proper actions.

## Getting started

You can deploy k8s-shredder in your cluster by using the [helm chart](charts/k8s-shredder).

Then, during a cluster upgrade, while rotating the worker nodes, you will need to label the nodes and the non-daemonset pods it contains with labels similar to this:

```bash
shredder.ethos.adobe.net/parked-by=k8s-shredder
shredder.ethos.adobe.net/parked-node-expires-on=1750164865.36373
shredder.ethos.adobe.net/upgrade-status=parked
```

You can either park (e.g. label the nodes and pods) with your own cluster management tooling, or you can configure k8s-shredder to park nodes for you based on [karpenter node claim status](#karpenter-integration) or the presence of a [specific label on the node](#labeled-node-detection).

The following options can be used to customize the k8s-shredder controller:

| Name                               | Default Value                                               | Description                                                                                          |
| :--------------------------------: | :---------------------------------------------------------: | :--------------------------------------------------------------------------------------------------: |
| EvictionLoopInterval               | 60s                                                         | How often to run the eviction loop process                                                           |
| ParkedNodeTTL                      | 60m                                                         | Time a node can be parked before starting force eviction process                                     |
| RollingRestartThreshold            | 0.5                                                         | How much time(percentage) should pass from ParkedNodeTTL before starting the rollout restart process |
| UpgradeStatusLabel                 | "shredder.ethos.adobe.net/upgrade-status"                   | Label used for the identifying parked nodes                                                          |
| ExpiresOnLabel                     | "shredder.ethos.adobe.net/parked-node-expires-on"           | Label used for identifying the TTL for parked nodes                                                  |
| NamespacePrefixSkipInitialEviction | ""                                                          | For pods in namespaces having this prefix proceed directly with a rollout restart without waiting for the RollingRestartThreshold |
| RestartedAtAnnotation              | "shredder.ethos.adobe.net/restartedAt"                      | Annotation name used to mark a controller object for rollout restart                                 |
| AllowEvictionLabel                 | "shredder.ethos.adobe.net/allow-eviction"                   | Label used for skipping evicting pods that have explicitly set this label on false                   |
| ToBeDeletedTaint                   | "ToBeDeletedByClusterAutoscaler"                            | Node taint used for skipping a subset of parked nodes that are already handled by cluster-autoscaler |
| ArgoRolloutsAPIVersion             | "v1alpha1"                                                  | API version from `argoproj.io` API group to be used while handling Argo Rollouts objects             |

| EnableKarpenterDriftDetection      | false                                                       | Controls whether to scan for drifted Karpenter NodeClaims and automatically label their nodes        |
| ParkedByLabel                      | "shredder.ethos.adobe.net/parked-by"                        | Label used to identify which component parked the node                                               |
| ParkedNodeTaint                    | "shredder.ethos.adobe.net/upgrade-status=parked:NoSchedule" | Taint to apply to parked nodes in format key=value:effect                                            |
| EnableNodeLabelDetection           | false                                                       | Controls whether to scan for nodes with specific labels and automatically park them                  |
| NodeLabelsToDetect                 | []                                                          | List of node labels to detect. Supports both key-only and key=value formats                          |

### How it works

k8s-shredder will periodically run eviction loops, based on configured `EvictionLoopInterval`, trying to clean up all the pods from the parked nodes. Once all the pods are cleaned up, [cluster-autoscaler](

https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) or [karpenter](https://github.com/kubernetes-sigs/karpenter) should chime in and recycle the parked node.

The diagram below describes a simple flow about how k8s-shredder handles stateful set applications:

<img src="docs/k8s-shredder.gif" alt="K8s-Shredder project"/>

#### Karpenter Integration

k8s-shredder includes an optional feature for automatic detection of drifted Karpenter NodeClaims. This feature is disabled by default, but can be enabled by setting `EnableKarpenterDriftDetection` to `true`. When enabled, at the beginning of each eviction loop, the controller will:

1. Scan the Kubernetes cluster for Karpenter NodeClaims that are marked as "Drifted"
2. Identify the nodes associated with these drifted NodeClaims
3. Automatically process these nodes by:

   - **Labeling** nodes and their non-DaemonSet pods with:
       - `UpgradeStatusLabel` (set to "parked") 
       - `ExpiresOnLabel` (set to current time + `ParkedNodeTTL`)
       - `ParkedByLabel` (set to "k8s-shredder")
   - **Cordoning** the nodes to prevent new pod scheduling
   - **Tainting** the nodes with the configured `ParkedNodeTaint`

This integration allows k8s-shredder to automatically handle node lifecycle management for clusters using Karpenter, ensuring that drifted nodes are properly marked for eviction and eventual replacement.

#### Labeled Node Detection

k8s-shredder includes optional automatic detection of nodes with specific labels. This feature is disabled by default but can be enabled by setting `EnableNodeLabelDetection` to `true`. When enabled, at the beginning of each eviction loop, the application will:

1. Scan the Kubernetes cluster for nodes that match any of the configured label selectors in `NodeLabelsToDetect`
2. Support both key-only (`"maintenance"`) and key=value (`"upgrade=required"`) label formats
3. Automatically process matching nodes by:

   - **Labeling** nodes and their non-DaemonSet pods with:
       - `UpgradeStatusLabel` (set to "parked") 
       - `ExpiresOnLabel` (set to current time + `ParkedNodeTTL`)
       - `ParkedByLabel` (set to "k8s-shredder")
   - **Cordoning** the nodes to prevent new pod scheduling
   - **Tainting** the nodes with the configured `ParkedNodeTaint`

This integration allows k8s-shredder to automatically handle node lifecycle management based on custom labeling strategies, enabling teams to mark nodes for parking using their own operational workflows and labels.  For example, this can be used in conjunction with [AKS cluster upgrades](https://learn.microsoft.com/en-us/azure/aks/upgrade-cluster#set-new-cordon-behavior).

#### Creating a new release

See [RELEASE.md](RELEASE.md).