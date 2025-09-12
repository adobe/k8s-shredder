// Copyright 2025 Adobe. All rights reserved.
package main

import (
	"flag"
	"log"
	"os"

	e2e "github.com/adobe/k8s-shredder/internal/testing"
)

func main() {
	var nodeName, kubeconfigPath string

	// Use a custom flag set to avoid conflicts with client-go flags
	fs := flag.NewFlagSet("park-node", flag.ExitOnError)
	fs.StringVar(&nodeName, "node", "", "Name of the node to park")
	fs.StringVar(&kubeconfigPath, "park-kubeconfig", "", "Path to kubeconfig file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	if nodeName == "" {
		log.Fatal("Node name is required. Use -node flag")
	}
	if kubeconfigPath == "" {
		log.Fatal("Kubeconfig path is required. Use -park-kubeconfig flag")
	}

	if err := e2e.ParkNodeForTesting(nodeName, kubeconfigPath); err != nil {
		log.Fatalf("Failed to park node: %v", err)
	}

	log.Printf("Successfully parked node %s", nodeName)
	os.Exit(0)
}
