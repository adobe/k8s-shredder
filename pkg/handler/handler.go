/*
Copyright 2022 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/adobe/k8s-shredder/pkg/metrics"
	"github.com/adobe/k8s-shredder/pkg/utils"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	deploymentutil "k8s.io/kubectl/pkg/util/deployment"
	"k8s.io/utils/ptr"
)

// Handler encapsulates the logic of the eviction loop
type Handler struct {
	appContext *utils.AppContext
	logger     *log.Entry
}

type controllerObject struct {
	Kind      string
	Name      string
	Namespace string
	Object    runtime.Object
}

func newControllerObject(kind, name, namespace string, obj runtime.Object) *controllerObject {
	return &controllerObject{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
		Object:    obj,
	}
}

func (co *controllerObject) Fingerprint() string {
	return fmt.Sprintf("%s/%s/%s", co.Kind, co.Namespace, co.Name)
}

// NewHandler returns a new Handler for the given application context
func NewHandler(appContext *utils.AppContext) *Handler {
	logger := log.WithField("dryRun", appContext.IsDryRun())
	return &Handler{appContext: appContext, logger: logger}
}

// Run starts an eviction loop
func (h *Handler) Run() error {
	// start measuring the loop duration
	loopTimer := prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		metrics.ShredderLoopsDurationSeconds.Observe(v * 10e6)
	}))

	// reset gauge metrics
	metrics.ShredderNodeForceToEvictTime.Reset()
	metrics.ShredderPodForceToEvictTime.Reset()
	metrics.ShredderPodErrorsTotal.Reset()

	h.logger.Infof("Starting eviction loop")

	// First, scan for drifted Karpenter node claims and label their nodes (if enabled)
	if h.appContext.Config.EnableKarpenterDriftDetection {
		err := utils.ProcessDriftedKarpenterNodes(h.appContext.Context, h.appContext, h.logger)
		if err != nil {
			h.logger.WithError(err).Warn("Failed to process drifted Karpenter nodes, continuing with normal eviction loop")
			metrics.ShredderErrorsTotal.Inc()
			// We don't return here because we want to continue with the normal eviction loop even if Karpenter drift detection fails
		}
	} else {
		h.logger.Debug("Karpenter drift detection is disabled")
	}

	// Second, scan for nodes with specific labels and park them (if enabled)
	if h.appContext.Config.EnableNodeLabelDetection {
		err := utils.ProcessNodesWithLabels(h.appContext.Context, h.appContext, h.logger)
		if err != nil {
			h.logger.WithError(err).Warn("Failed to process nodes with label detection, continuing with normal eviction loop")
			metrics.ShredderErrorsTotal.Inc()
			// We don't return here because we want to continue with the normal eviction loop even if node label detection fails
		}
	} else {
		h.logger.Debug("Node label detection is disabled")
	}

	// sync all nodes goroutines
	wg := sync.WaitGroup{}
	// rr channel is used to pass controller objects to be restarted by the rollout restart goroutine
	rr := make(chan *controllerObject, 50)
	// done and doneBack are used to signal rollout restart goroutine to finish its execution
	done := make(chan bool)
	doneBack := make(chan bool)

	defer func() {
		wg.Wait()
		done <- true
		<-doneBack
		close(rr)
		close(done)
		close(doneBack)
	}()

	// first start the rollout restart goroutine so that it is ready to receive controller objects to be restarted
	go h.rolloutRestart(rr, done, doneBack)

	nodeList, err := h.getParkedNodes()
	if err != nil {
		h.logger.Errorf("%s", err.Error())
		metrics.ShredderErrorsTotal.Inc()
		loopTimer.ObserveDuration()
		return err
	}

	h.logger.Debugf("Found %d matching nodes (parked)", len(nodeList.Items))

	for _, node := range nodeList.Items {
		if utils.NodeHasTaint(node, h.appContext.Config.ToBeDeletedTaint) {
			// skip nodes with "ToBeDeletedByClusterAutoscaler" taint
			h.logger.Debugf("Skipping node %s with taint %s", node.Name, h.appContext.Config.ToBeDeletedTaint)
			continue
		}

		// start a new goroutine for every parked node
		wg.Add(1)
		go func(node v1.Node, wg *sync.WaitGroup) {
			defer wg.Done()
			err := h.processNode(node, rr)
			if err != nil {
				h.logger.Errorf("%s", err.Error())
				metrics.ShredderErrorsTotal.Inc()
			}
		}(node, &wg)
		metrics.ShredderProcessedNodesTotal.Inc()
	}

	metrics.ShredderLoopsTotal.Inc()
	loopTimer.ObserveDuration()
	return nil
}

// processNode performs the eviction logic for a single node
func (h *Handler) processNode(node v1.Node, rr chan *controllerObject) error {
	h.logger.Infof("Processing node %s", node.Name)

	if !utils.NodeHasLabel(node, h.appContext.Config.ExpiresOnLabel) {
		return errors.Errorf("Node %s missing required label %s", node.Name, h.appContext.Config.ExpiresOnLabel)
	}

	expiresOn, err := utils.GetParkedNodeExpiryTime(node, h.appContext.Config.ExpiresOnLabel)
	if err != nil {
		return err
	}

	h.logger.Debugf("Parked node %s expires on %s", node.Name, expiresOn.String())
	metrics.ShredderNodeForceToEvictTime.WithLabelValues(node.Name).Set(float64(expiresOn.Unix()))

	deletePropagationBackground := metav1.DeletePropagationBackground
	deleteOptions := &metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	}

	if h.appContext.IsDryRun() {
		deleteOptions.DryRun = []string{metav1.DryRunAll}
	}

	podList, err := h.GetPodsForNode(node)
	if err != nil {
		return err
	}

	h.logger.Debugf("Found %d eligible for evict pods on parked node %s", len(podList), node.Name)

	if time.Now().UTC().After(expiresOn) {
		h.logger.Infof("Force evicting pods from expired parked node %s", node.Name)

		deleteOptions.GracePeriodSeconds = ptr.To[int64](0)

		for _, pod := range podList {
			err = h.deletePod(pod, deleteOptions)
			if err != nil {
				h.logger.WithFields(log.Fields{
					"namespace": pod.Namespace,
					"pod":       pod.Name,
				}).Warnf("Failed to delete pod: %s", err.Error())
				continue
			}
			metrics.ShredderProcessedPodsTotal.Inc()
		}

		return nil
	}

	for _, pod := range podList {
		metrics.ShredderPodForceToEvictTime.WithLabelValues(pod.Name, pod.Namespace).Set(float64(expiresOn.Unix()))

		if !utils.PodEvictionAllowed(pod, h.appContext.Config.AllowEvictionLabel) {
			h.logger.Debugf("Skipping %s as it has '%s=false' label set", pod.Name, h.appContext.Config.AllowEvictionLabel)
			continue
		}

		if h.appContext.Config.NamespacePrefixSkipInitialEviction == "" || !strings.HasPrefix(pod.Namespace, h.appContext.Config.NamespacePrefixSkipInitialEviction) {
			rrThresholdTime := h.appContext.Config.ParkedNodeTTL * time.Duration(100-h.appContext.Config.RollingRestartThreshold*100) / 100
			if time.Now().UTC().Before(expiresOn.Add(-rrThresholdTime)) {
				err := h.evictPod(pod, deleteOptions)
				if err != nil {
					h.logger.WithFields(log.Fields{
						"namespace": pod.Namespace,
						"pod":       pod.Name,
					}).Warnf("Failed to evict pod: %s", err.Error())
				}
				continue
			}
		}

		co, err := h.getControllerObject(pod)
		if err != nil {
			h.logger.WithFields(log.Fields{
				"namespace": pod.Namespace,
				"pod":       pod.Name,
			}).Warnf("Failed to get pod controller object: %s. Proceeding directly with pod eviction", err.Error())
			err := h.evictPod(pod, deleteOptions)
			if err != nil {
				h.logger.WithFields(log.Fields{
					"namespace": pod.Namespace,
					"pod":       pod.Name,
				}).Warnf("Failed to evict pod: %s", err.Error())
			}
			continue
		}

		// For pods handled by a deployment, statefulset or argo rollouts controller, try to rollout restart those objects
		if slices.Contains([]string{"Deployment", "StatefulSet", "Rollout"}, co.Kind) {
			rolloutRestartInProgress, err := h.isRolloutRestartInProgress(co)
			if err != nil {
				h.logger.WithField("key", co.Fingerprint()).Warnf("Failed to get rollout status: %s", err.Error())
				metrics.ShredderErrorsTotal.Inc()
				continue
			}
			// if the rollout restart process is in progress, evict the pod instead of trying to do another rollout restart
			if rolloutRestartInProgress {
				err := h.evictPod(pod, deleteOptions)
				if err != nil {
					h.logger.WithFields(log.Fields{
						"namespace": pod.Namespace,
						"pod":       pod.Name,
					}).Warnf("Failed to evict pod: %s", err.Error())
				}
				continue
			}

			// If there isn't any rollout in progress, send the controller object into the rollout restart channel in order to
			// be processed by the rolloutRestart goroutine
			rr <- co
		}
		metrics.ShredderProcessedPodsTotal.Inc()
	}

	return nil
}

// getParkedNodes queries the APIServer for a list of nodes that have the parked label set
func (h *Handler) getParkedNodes() (*v1.NodeList, error) {
	labelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			h.appContext.Config.UpgradeStatusLabel: "parked",
		},
	}

	nodeList, err := h.appContext.K8sClient.CoreV1().Nodes().List(h.appContext.Context, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})

	if err != nil {
		return nil, err
	}

	return nodeList, nil
}

// GetPodsForNode returns all eligible for evict pods from a specific node
func (h *Handler) GetPodsForNode(node v1.Node) ([]v1.Pod, error) {
	podList, err := h.appContext.K8sClient.CoreV1().Pods("").List(h.appContext.Context, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", node.Name),
	})

	if err != nil {
		return nil, err
	}

	var podListCleaned []v1.Pod

	// we need to remove any non-eligible pods
	for _, pod := range podList.Items {
		// skip pods in terminating state
		if pod.DeletionTimestamp != nil {
			h.logger.Debugf("Skipping %s as it is in terminating state", pod.Name)
			continue
		}

		// skip pods with DaemonSet controller object or static pods
		if len(pod.OwnerReferences) > 0 && slices.Contains([]string{"DaemonSet", "Node"}, pod.ObjectMeta.OwnerReferences[0].Kind) {
			h.logger.Debugf("Skipping %s as it is part of a DaemonSet or is a static pod", pod.Name)
			continue
		}

		podListCleaned = append(podListCleaned, pod)
	}

	return podListCleaned, nil
}

// evictPod evict a pod using the eviction API
func (h *Handler) evictPod(pod v1.Pod, deleteOptions *metav1.DeleteOptions) error {
	h.logger.Infof("Evicting pod %s from %s namespace", pod.Name, pod.Namespace)
	err := h.appContext.K8sClient.PolicyV1().Evictions(pod.Namespace).Evict(h.appContext.Context, &policy.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		DeleteOptions: deleteOptions,
	})

	if err != nil {
		metrics.ShredderPodErrorsTotal.WithLabelValues(pod.Name, pod.Namespace, err.Error(), "evict")
		return err
	}

	return nil
}

// deletePod deletes a pod using the delete options
func (h *Handler) deletePod(pod v1.Pod, deleteOptions *metav1.DeleteOptions) error {
	coreClient := h.appContext.K8sClient.CoreV1()

	h.logger.Infof("Deleting pod %s from %s namespace", pod.Name, pod.Namespace)
	err := coreClient.Pods(pod.Namespace).Delete(h.appContext.Context, pod.Name, *deleteOptions)

	if err != nil {
		metrics.ShredderPodErrorsTotal.WithLabelValues(pod.Name, pod.Namespace, err.Error(), "delete")
		return err
	}

	return nil
}

func (h *Handler) getControllerObject(pod v1.Pod) (*controllerObject, error) {
	co := newControllerObject("Unknown", "", "", nil)

	if len(pod.OwnerReferences) == 0 {
		h.logger.Warnf("Pod %s has no owner", pod.Name)
		return co, nil
	}

	switch pod.OwnerReferences[0].Kind {
	case "ReplicaSet":
		replicaSet, err := h.appContext.K8sClient.AppsV1().ReplicaSets(pod.Namespace).Get(h.appContext.Context, pod.OwnerReferences[0].Name, metav1.GetOptions{})
		if err != nil {
			return co, err
		}
		co = newControllerObject("ReplicaSet", replicaSet.Name, replicaSet.Namespace, replicaSet)
		if len(replicaSet.OwnerReferences) == 0 {
			h.logger.Warnf("Pod %s is controlled by an isolated ReplicaSet", pod.Name)
			return co, nil
		}

		switch replicaSet.OwnerReferences[0].Kind {
		case "Deployment":

			deployment, err := h.appContext.K8sClient.AppsV1().Deployments(pod.Namespace).Get(h.appContext.Context, replicaSet.OwnerReferences[0].Name, metav1.GetOptions{})
			if err != nil {
				return co, err
			}
			return newControllerObject("Deployment", deployment.Name, deployment.Namespace, deployment), nil
		case "Rollout":
			// Make sure we are dealing with an Argo Rollout
			if replicaSet.OwnerReferences[0].APIVersion == fmt.Sprintf("argoproj.io/%s", h.appContext.Config.ArgoRolloutsAPIVersion) {

				gvr := schema.GroupVersionResource{
					Group:    "argoproj.io",
					Version:  h.appContext.Config.ArgoRolloutsAPIVersion,
					Resource: "rollouts",
				}

				rollout, err := h.appContext.DynamicK8SClient.Resource(gvr).Namespace(pod.Namespace).Get(h.appContext.Context, replicaSet.OwnerReferences[0].Name, metav1.GetOptions{})
				if err != nil {
					return co, err
				}
				return newControllerObject("Rollout", rollout.GetName(), rollout.GetNamespace(), rollout), nil
			} else {
				return co, errors.Errorf("Controller object of type %s from %s API group is not supported! Please file a git issue or contribute it!", replicaSet.OwnerReferences[0].Kind, replicaSet.OwnerReferences[0].APIVersion)
			}
		default:
			return co, errors.Errorf("Controller object of type %s from %s API group is not supported! Please file a git issue or contribute it!", pod.OwnerReferences[0].Kind, pod.OwnerReferences[0].APIVersion)
		}

	case "DaemonSet":
		h.logger.Warnf("DaemonSets are not covered")
		return newControllerObject("DaemonSet", "", "", nil), nil

	case "Node":
		h.logger.Warnf("Static pods are not covered")
		return newControllerObject("StaticPod", "", "", nil), nil

	case "StatefulSet":
		sts, err := h.appContext.K8sClient.AppsV1().StatefulSets(pod.Namespace).Get(h.appContext.Context, pod.OwnerReferences[0].Name, metav1.GetOptions{})
		if err != nil {
			return co, err
		}
		return newControllerObject("StatefulSet", sts.Name, sts.Namespace, sts), nil
	default:
		return co, errors.Errorf("Controller object of type %s is not a standard controller", pod.OwnerReferences[0].Kind)
	}
}

func (h *Handler) isRolloutRestartInProgress(co *controllerObject) (bool, error) {
	switch co.Kind {
	case "Deployment":
		deployment := co.Object.(*appsv1.Deployment)

		// first check if deployment exceeded its rollout progress deadline
		cond := deploymentutil.GetDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)
		if cond != nil && cond.Reason == deploymentutil.TimedOutReason {
			return false, nil
		}

		// second validate if there is any in progress rollout
		return deployment.Generation <= deployment.Status.ObservedGeneration &&
			(deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas) ||
			(deployment.Status.Replicas > deployment.Status.UpdatedReplicas) ||
			(deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas), nil

	case "StatefulSet":
		sts := co.Object.(*appsv1.StatefulSet)

		if sts.Spec.UpdateStrategy.Type != appsv1.RollingUpdateStatefulSetStrategyType {
			h.logger.Warnf("Rollout status is only available for %s strategy type", appsv1.RollingUpdateStatefulSetStrategyType)
			return false, nil
		}
		if sts.Status.ObservedGeneration == 0 || sts.Generation > sts.Status.ObservedGeneration {
			h.logger.Warnf("StatefulSet %s has not yet been observed", sts.Name)
			return false, nil
		}
		if sts.Spec.Replicas != nil && sts.Status.ReadyReplicas < *sts.Spec.Replicas {
			return true, nil
		}
		if sts.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType && sts.Spec.UpdateStrategy.RollingUpdate != nil {
			if sts.Spec.Replicas != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
				if sts.Status.UpdatedReplicas < (*sts.Spec.Replicas - *sts.Spec.UpdateStrategy.RollingUpdate.Partition) {
					return true, nil
				}
			}
		}
		if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
			return true, nil
		}
	case "Rollout":
		rollout := co.Object.(*unstructured.Unstructured)

		// TODO - check if the other rollout conditions should be checked as well
		// See https://github.com/argoproj/argo-rollouts/blob/bfef7f0d2bb71b085398c35ec95c1b2aacd07187/rollout/sync.go#L618
		isPaused, found, err := unstructured.NestedBool(rollout.Object, "spec", "paused")
		if err != nil {
			return false, err
		}

		if found && isPaused {
			h.logger.Warnf("Argo Rollout %s is currently paused, won't restart it!", rollout.GetName())
			return false, nil
		}
	default:
		return false, errors.Errorf("rollout restart not supported for object of type %s", co.Kind)
	}

	return false, nil
}

func (h *Handler) rolloutRestart(rr chan *controllerObject, done, doneBack chan bool) {
	processed := map[string]bool{}
	for {
		select {
		case co := <-rr:
			key := co.Fingerprint()

			if _, ok := processed[key]; ok {
				h.logger.
					WithField("key", key).
					Debugf("Controller object already processed")
				break
			}

			processed[key] = true

			rolloutRestartInProgress, err := h.isRolloutRestartInProgress(co)
			if err != nil {
				h.logger.
					WithField("key", key).
					Warnf("Failed to get rollout status: %s", err.Error())
				metrics.ShredderErrorsTotal.Inc()
				break
			}

			if rolloutRestartInProgress {
				h.logger.
					WithField("key", key).
					Debug("Rollout restart already in progress")
				break
			}

			err = h.doRolloutRestart(co)
			if err != nil {
				h.logger.
					WithField("key", key).
					Warnf("Failed to perform rollout restart: %s", err.Error())
				metrics.ShredderErrorsTotal.Inc()
			}

		case <-done:
			h.logger.Debugf("See you next time!")
			doneBack <- true
			return
		}
	}
}

func (h *Handler) doRolloutRestart(co *controllerObject) error {
	h.logger.
		WithField("fingerprint", co.Fingerprint()).
		Infof("Performing rollout restart")

	patchOptions := metav1.PatchOptions{
		FieldManager: "k8s-shredder",
	}
	if h.appContext.IsDryRun() {
		patchOptions.DryRun = []string{metav1.DryRunAll}
	}

	restartedAt := time.Now().UTC().Format(time.RFC3339)
	patchData, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]map[string]string{
					"annotations": {
						h.appContext.Config.RestartedAtAnnotation: restartedAt,
					},
				},
			},
		},
	})

	switch co.Kind {
	case "Deployment":
		deployment := co.Object.(*appsv1.Deployment)
		_, err := h.appContext.K8sClient.AppsV1().Deployments(deployment.Namespace).
			Patch(h.appContext.Context, deployment.Name, types.StrategicMergePatchType, patchData, patchOptions)
		if err != nil {
			return err
		}
	case "StatefulSet":
		sts := co.Object.(*appsv1.StatefulSet)
		_, err := h.appContext.K8sClient.AppsV1().StatefulSets(sts.Namespace).
			Patch(h.appContext.Context, sts.Name, types.StrategicMergePatchType, patchData, patchOptions)
		if err != nil {
			return err
		}
	case "Rollout":
		rollout := co.Object.(*unstructured.Unstructured)
		gvr := schema.GroupVersionResource{
			Group:    "argoproj.io",
			Version:  h.appContext.Config.ArgoRolloutsAPIVersion,
			Resource: "rollouts",
		}

		patchDataRollout, _ := json.Marshal(map[string]interface{}{
			"spec": map[string]interface{}{
				"restartAt": restartedAt,
			},
		})

		_, err := h.appContext.DynamicK8SClient.Resource(gvr).Namespace(rollout.GetNamespace()).Patch(h.appContext.Context, rollout.GetName(), types.MergePatchType, patchDataRollout, patchOptions)
		if err != nil {
			return err
		}
	case "DaemonSet":
		return errors.Errorf("DaemonSets are not covered")
	default:
		return errors.Errorf("invalid controller object")
	}
	return nil
}
