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

package utils

import (
	"context"
	"github.com/adobe/k8s-shredder/pkg/config"
	"k8s.io/client-go/dynamic"

	"k8s.io/client-go/kubernetes"
)

// AppContext struct stores a context and a k8s client
type AppContext struct {
	Context          context.Context
	K8sClient        kubernetes.Interface
	DynamicK8SClient dynamic.Interface
	Config           config.Config
	dryRun           bool
}

// NewAppContext creates a new AppContext object
func NewAppContext(cfg config.Config, dryRun bool) (*AppContext, error) {
	client, err := getK8SClient()
	if err != nil {
		return nil, err
	}

	dynamicClient, err := getDynamicK8SClient()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	go HandleOsSignals(cancel)

	return &AppContext{
		Context:          ctx,
		K8sClient:        client,
		DynamicK8SClient: dynamicClient,
		Config:           cfg,
		dryRun:           dryRun,
	}, nil
}

// IsDryRun returns true if the "--dry-run" flag was provided
func (ac *AppContext) IsDryRun() bool {
	return ac.dryRun
}
