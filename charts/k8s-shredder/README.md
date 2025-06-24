# k8s-shredder

![Version: 0.2.2](https://img.shields.io/badge/Version-0.2.2-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.3.1](https://img.shields.io/badge/AppVersion-v0.3.1-informational?style=flat-square)

a novel way of dealing with kubernetes nodes blocked from draining

**Homepage:** <https://github.com/adobe/k8s-shredder>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| adriananeci | <aneci@adobe.com> | <https://adobe.com> |
| sfotony | <gosselin@adobe.com> | <https://adobe.com> |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| additionalContainers | list | `[]` | Additional containers to run alongside k8s-shredder in the same pod |
| affinity | object | `{}` | Affinity rules for advanced pod scheduling (node affinity, pod affinity/anti-affinity) |
| deploymentStrategy | object | `{}` | Deployment strategy for rolling updates (e.g., RollingUpdate, Recreate) |
| dryRun | bool | `false` | Enable dry-run mode - when true, k8s-shredder will log actions but not execute them |
| environmentVars | list | `[]` | Additional environment variables to set in the container |
| fullnameOverride | string | `""` | Override the full name used for resources |
| image | object | `{"pullPolicy":"IfNotPresent","registry":"ghcr.io/adobe/k8s-shredder"}` | Container image configuration |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy - IfNotPresent, Always, or Never |
| image.registry | string | `"ghcr.io/adobe/k8s-shredder"` | Container registry where the k8s-shredder image is hosted |
| imagePullSecrets | list | `[]` | Secrets for pulling images from private registries |
| initContainers | list | `[]` | Init containers to run before the main k8s-shredder container starts |
| logFormat | string | `"text"` | Log output format: text (human-readable) or json (structured logging) |
| logLevel | string | `"debug"` | Available log levels: panic, fatal, error, warn, warning, info, debug, trace |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector to constrain pod scheduling to specific nodes |
| podAnnotations | object | `{}` | Annotations to add to k8s-shredder pod(s) |
| podLabels | object | `{}` | Additional labels to add to k8s-shredder pod(s) |
| podMonitor | object | `{"enabled":false,"honorLabels":true,"interval":"60s","labels":{},"relabelings":[],"scrapeTimeout":"10s"}` | Prometheus monitoring configuration |
| podMonitor.enabled | bool | `false` | Enable creation of a PodMonitor resource for Prometheus scraping |
| podMonitor.honorLabels | bool | `true` | Whether to honor labels from the target |
| podMonitor.interval | string | `"60s"` | How often Prometheus should scrape metrics |
| podMonitor.labels | object | `{}` | Labels to apply to the PodMonitor resource |
| podMonitor.relabelings | list | `[]` | Metric relabeling configuration |
| podMonitor.scrapeTimeout | string | `"10s"` | Timeout for each scrape attempt |
| podSecurityContext | object | `{}` | Security context applied to the entire pod |
| priorityClassName | string | `"system-cluster-critical"` | Priority class for pod scheduling - system-cluster-critical ensures high priority |
| rbac | object | `{"create":true}` | RBAC (Role-Based Access Control) configuration |
| rbac.create | bool | `true` | Create RBAC resources (ClusterRole, ClusterRoleBinding) |
| replicaCount | int | `1` | Number of k8s-shredder pods to run |
| resources | object | `{"limits":{"cpu":"1","memory":"1Gi"},"requests":{"cpu":"250m","memory":"250Mi"}}` | Resource requests and limits for the k8s-shredder container |
| resources.limits.cpu | string | `"1"` | Maximum CPU cores the container can use |
| resources.limits.memory | string | `"1Gi"` | Maximum memory the container can use |
| resources.requests.cpu | string | `"250m"` | CPU cores requested for the container (guaranteed allocation) |
| resources.requests.memory | string | `"250Mi"` | Memory requested for the container (guaranteed allocation) |
| securityContext | object | `{}` | Security context applied to the k8s-shredder container |
| serviceAccount | object | `{"annotations":{},"create":true,"name":"k8s-shredder"}` | Kubernetes service account configuration |
| serviceAccount.annotations | object | `{}` | Additional annotations for the service account (useful for IAM roles, etc.) |
| serviceAccount.create | bool | `true` | Create a service account for k8s-shredder |
| serviceAccount.name | string | `"k8s-shredder"` | Name of the service account |
| shredder | object | `{"AllowEvictionLabel":"shredder.ethos.adobe.net/allow-eviction","ArgoRolloutsAPIVersion":"v1alpha1","EnableKarpenterDriftDetection":false,"EnableNodeLabelDetection":false,"EvictionLoopInterval":"1h","ExpiresOnLabel":"shredder.ethos.adobe.net/parked-node-expires-on","NamespacePrefixSkipInitialEviction":"ns-ethos-","NodeLabelsToDetect":[],"ParkedByLabel":"shredder.ethos.adobe.net/parked-by","ParkedByValue":"k8s-shredder","ParkedNodeTTL":"168h","ParkedNodeTaint":"shredder.ethos.adobe.net/upgrade-status=parked:NoSchedule","RestartedAtAnnotation":"shredder.ethos.adobe.net/restartedAt","RollingRestartThreshold":0.1,"ToBeDeletedTaint":"ToBeDeletedByClusterAutoscaler","UpgradeStatusLabel":"shredder.ethos.adobe.net/upgrade-status"}` | Core k8s-shredder configuration |
| shredder.AllowEvictionLabel | string | `"shredder.ethos.adobe.net/allow-eviction"` | Label to explicitly allow eviction on specific resources |
| shredder.ArgoRolloutsAPIVersion | string | `"v1alpha1"` | API version for Argo Rollouts integration |
| shredder.EnableKarpenterDriftDetection | bool | `false` | Enable Karpenter drift detection for node lifecycle management |
| shredder.EnableNodeLabelDetection | bool | `false` | Enable detection of nodes based on specific labels |
| shredder.EvictionLoopInterval | string | `"1h"` | How often to run the main eviction loop |
| shredder.ExpiresOnLabel | string | `"shredder.ethos.adobe.net/parked-node-expires-on"` | Label used to track when a parked node expires |
| shredder.NamespacePrefixSkipInitialEviction | string | `"ns-ethos-"` | Namespace prefix to skip during initial eviction (useful for system namespaces) |
| shredder.NodeLabelsToDetect | list | `[]` | List of node labels to monitor for triggering shredder actions |
| shredder.ParkedByLabel | string | `"shredder.ethos.adobe.net/parked-by"` | Label to track which component parked a node |
| shredder.ParkedByValue | string | `"k8s-shredder"` | Value set in the ParkedByLabel to identify k8s-shredder as the parking agent |
| shredder.ParkedNodeTTL | string | `"168h"` | How long parked nodes should remain before being eligible for deletion (7 days default) |
| shredder.ParkedNodeTaint | string | `"shredder.ethos.adobe.net/upgrade-status=parked:NoSchedule"` | Taint applied to parked nodes to prevent new pod scheduling |
| shredder.RestartedAtAnnotation | string | `"shredder.ethos.adobe.net/restartedAt"` | Annotation to track when a workload was last restarted |
| shredder.RollingRestartThreshold | float | `0.1` | Maximum percentage of nodes that can be restarted simultaneously during rolling restarts |
| shredder.ToBeDeletedTaint | string | `"ToBeDeletedByClusterAutoscaler"` | Taint indicating nodes scheduled for deletion by cluster autoscaler |
| shredder.UpgradeStatusLabel | string | `"shredder.ethos.adobe.net/upgrade-status"` | Label used to track node upgrade status |
| tolerations | list | `[]` | Tolerations to allow scheduling on nodes with specific taints |
| topologySpreadConstraints | list | `[]` | Helps ensure high availability by spreading pods across zones/nodes |
| volumes | list | `[]` | Additional volumes to mount in the pod |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
