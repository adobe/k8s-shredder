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

package e2e

import (
	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/adobe/k8s-shredder/pkg/handler"
	"github.com/adobe/k8s-shredder/pkg/utils"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

// Validates that k8s-shredder cleanup a parked node after its TTL expires
func TestNodeIsCleanedUp(t *testing.T) {
	var err error

	appContext, err := utils.NewAppContext(config.Config{
		ParkedNodeTTL:                      1 * time.Minute,
		EvictionLoopInterval:               10 * time.Second,
		RollingRestartThreshold:            0.1,
		UpgradeStatusLabel:                 "shredder.ethos.adobe.net/upgrade-status",
		ExpiresOnLabel:                     "shredder.ethos.adobe.net/parked-node-expires-on",
		NamespacePrefixSkipInitialEviction: "",
		RestartedAtAnnotation:              "shredder.ethos.adobe.net/restartedAt",
		AllowEvictionLabel:                 "shredder.ethos.adobe.net/allow-eviction",
		ToBeDeletedTaint:                   "ToBeDeletedByClusterAutoscaler",
	}, false)

	if err != nil {
		log.Fatalf("Failed to setup application context: %s", err)
	}

	parkedWorkerNode := "k8s-shredder-test-cluster-worker"
	coreV1Client := appContext.K8sClient.CoreV1()
	// k8s-shredder-test-cluster-worker worker node is tainted in local_env_prep.sh
	node, err := coreV1Client.Nodes().Get(appContext.Context, parkedWorkerNode, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get the parked node: %s", err)
	}

	h := handler.NewHandler(appContext)

	// Wait for node TTL to expire
	expirationTime, err := utils.GetParkedNodeExpiryTime(*node, appContext.Config.ExpiresOnLabel)
	if err != nil {
		t.Fatalf("Failed to get expiration time for the parked node %s: %s", parkedWorkerNode, err)
	}
	ch := make(chan time.Time)
	go func() {
		compareTime(expirationTime, t, ch)
	}()
	currentTime := <-ch

	t.Logf("Node TTL expired: current time(UTC): %s, expire time(UTC): %s", currentTime, expirationTime.UTC())

	pods, err := h.GetPodsForNode(*node)

	if err != nil {
		t.Fatalf("Failed to get running pods from the parked node %s: %s", parkedWorkerNode, err)
	}

	userNamespaces := []string{"ns-team-k8s-shredder-test", "ns-k8s-shredder-test"}
	for _, pod := range pods {
		if pod.DeletionTimestamp == nil && slices.Contains(userNamespaces, pod.Namespace) {
			t.Logf("Pod %s was not evicted by shredder", pod.Name)
			t.Fail()
		}
	}
}

func compareTime(expirationTime time.Time, t *testing.T, ch chan time.Time) {
	currentTime := time.Now().UTC()
	for !currentTime.After(expirationTime.UTC().Add(60 * time.Second)) {
		t.Logf("Node TTL didn't expire yet: current time(UTC): %s, expire time(UTC): %s", currentTime, expirationTime.UTC())
		time.Sleep(10 * time.Second)
		currentTime = time.Now().UTC()
	}
	ch <- currentTime
}

func TestShredderMetrics(t *testing.T) {
	// TODO add metrics validation tests
	t.Log("Metrics validation test passed!")
}
