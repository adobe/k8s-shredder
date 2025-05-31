# Node Parking

"Node Parking" is a process by which nodes that need replacement but are currently being handled by which nodes and the pods scheduled on them are labeled and subsequently targeted for safe eviction over a period of (commonly) several days, after which the pods are forcibly removed by k8s-shredder and the node deleted by the cluster's autoscaler.  This process gives tenants the opportunity to reschedule sensitive workloads in a manner that fits their application's SLO while ultimately allowing for the eventual replacement of nodes.

## Parking Basics

When a cluster operator upgrades the node on a cluster (e.g. upgrades the version of Kubernetes, the underlying operating system, a configuration change, etc), it first needs to reschedule all pods on that node. This is done using the [Kuberentes Eviction API](https://kubernetes.io/docs/concepts/scheduling-eviction/api-eviction/), which is to say evictions that respect the application's [PodDisruptionBudgets](https://kubernetes.io/docs/tasks/run-application/configure-pdb/) (PDBs) and [terminationGracePeriodSeconds](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-termination) settings on its pods.  Once the node is emptied of application workloads, it is deleted by either the cluster-autoscaler or karpenter (or some other node autoscaler).

In some cases, it may not be possible to evict a pod without violating the PDB.  This is normally due to how the application owner has configured the PDB, but can be caused by other scenarios such as lack of nodes to schedule new pods of scale up due to cloud provider issues.  As stewards of the cluster's security and stability, cluster operators cannot let the node run forever.  However, they also want to make every effort to make sure application owners have a chance to own and manage their application stability.  Enter "Node Parking".

When a cluster operator encounters a node with a pod that cannot be evict, it will cordon and taint the node.  Then, it will label the node and the (non-daemonset) pods contained within it with the following labels:

```bash
shredder.ethos.adobe.net/parked-by=k8s-shredder
shredder.ethos.adobe.net/parked-node-expires-on=1750164865.36373
shredder.ethos.adobe.net/upgrade-status=parked
```

The first label denotes who/what parked the node.  The second contains a unix timestamp of when that node/pod may be forcibly removed.  The third label denotes that the node/pod is parked.  

These labels are used by a deployment called [k8s-shredder](../README.md) that will scan the cluster periodically for pods with these labels, and will try to evict them using the Eviction API.  After a portion of the expiry period has passed (default 10%), it will shift to using [rollout restarts](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_rollout/kubectl_rollout_restart/) to help reschedule the pod.  If the pod is still present when the expiration date is reached, it forcibly evicted (e.g. it is deleted); this the only action k8s-shredder will take that will violate the pod's PDB.

If you want a pod to be exempted from the eviction loop until parked node TTL expires, you can label the pod with

```bash
"shredder.ethos.adobe.net/allow-eviction=false"
```

so that k8s-shredder will skip it.  It will be encumbent on application owners to gracefully reschedule these pods to avoid deletion once the TTL expires.

More information about k8s-shredder and how it functions can be found [here](../README.md).

## How can I tell if my pods are parked?

As mentioned above, we don't want to forcibly evict our tenant's workloads, and we would much rather give them the power to manage the eviction process in a way that makes sense for their workload, SLOs, and customers.  Given that, we have exposed metrics and labels that will allow customers to track and alert when they have workloads that are parked so that they may take action.

### Metrics (Recommended)

If you are writing an alert or promql query, the recommended approach is to incorporate the metric `kube_ethos_upgrade:parked_pod` after exposing it in prometheus.  Given that the expiry time for a pod is measured in days, you may want to delay any alerting on pod-parking for the first hour or so to allow for normal rescheduling to occur.

### Pod Labels

Another way to find out if your pod is parked is to monitor the labels on the pods in your names space.  You can find parked pods using this kubectl command:

```
kubectl get pods -l shredder.ethos.adobe.net/upgrade-status=parked
```

You can also query and alert on pods labels (although, again, we recommend using the metric exposed above):

```
kube_pod_labels{label_shredder_ethos_adobe_net_upgrade_status="parked"}
```
