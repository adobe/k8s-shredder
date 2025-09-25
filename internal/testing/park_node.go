// Copyright 2025 Adobe. All rights reserved.
package e2e

import (
	"context"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/adobe/k8s-shredder/pkg/utils"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ParkNodeForTesting properly parks a node using the ParkNodes function
func ParkNodeForTesting(nodeName string, kubeconfigPath string) error {
	// Load kubeconfig from file without registering flags
	kubeconfig, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return err
	}

	k8sConfig, err := clientcmd.NewDefaultClientConfig(*kubeconfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return err
	}

	// Create Kubernetes client
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return err
	}

	// Create test configuration
	cfg := config.Config{
		ParkedNodeTTL:       1 * time.Minute, // 1 minute TTL for testing
		UpgradeStatusLabel:  "shredder.ethos.adobe.net/upgrade-status",
		ExpiresOnLabel:      "shredder.ethos.adobe.net/parked-node-expires-on",
		ParkedByLabel:       "shredder.ethos.adobe.net/parked-by",
		ParkedByValue:       "k8s-shredder",
		ParkedNodeTaint:     "shredder.ethos.adobe.net/upgrade-status=parked:NoSchedule",
		EvictionSafetyCheck: true, // Keep safety check enabled
		ExtraParkingLabels:  map[string]string{},
		ParkingReasonLabel:  "shredder.ethos.adobe.net/parked-reason",
	}

	// Create logger
	logEntry := log.NewEntry(log.New())

	// Create node info for parking
	nodesToPark := []utils.NodeInfo{
		{
			Name:   nodeName,
			Labels: map[string]string{},
		},
	}

	// Park the node (this will label both node and pods)
	ctx := context.Background()
	err = utils.ParkNodes(ctx, clientset, nodesToPark, cfg, false, "e2e-test", logEntry)
	if err != nil {
		return err
	}

	logEntry.Infof("Successfully parked node %s with proper pod labeling", nodeName)
	return nil
}
