/*
Copyright 2025 Adobe. All rights reserved.
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// fastLeaderElectionConfig returns timing values that are fast enough to keep
// unit tests short while still satisfying leaderelection.NewLeaderElector's
// validation constraints (LeaseDuration > RenewDeadline > RetryPeriod*JitterFactor).
func fastLeaderElectionConfig() config.Config {
	return config.Config{
		LeaderElectionLockName:      "test-lock",
		LeaderElectionLeaseDuration: 400 * time.Millisecond,
		LeaderElectionRenewDeadline: 200 * time.Millisecond,
		LeaderElectionRetryPeriod:   50 * time.Millisecond,
	}
}

func TestRunLeaderElection_BecomesLeaderAndStops(t *testing.T) {
	cfg := fastLeaderElectionConfig()
	cfg.LeaderElectionID = "test-identity"

	fakeClient := fake.NewClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ac := &AppContext{}

	var startedLeading, stoppedLeading atomic.Bool

	done := make(chan error, 1)
	go func() {
		done <- ac.RunLeaderElection(ctx, fakeClient, cfg,
			func() { startedLeading.Store(true) },
			func() { stoppedLeading.Store(true) },
		)
	}()

	require.Eventually(t, startedLeading.Load, 2*time.Second, 10*time.Millisecond, "expected to become leader")

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunLeaderElection did not return after context cancellation")
	}

	assert.True(t, stoppedLeading.Load(), "expected onStoppedLeading to be called")
}

func TestRunLeaderElection_UsesConfiguredIdentity(t *testing.T) {
	cfg := fastLeaderElectionConfig()
	cfg.LeaderElectionID = "my-custom-identity"
	cfg.LeaderElectionNamespace = "custom-ns"

	fakeClient := fake.NewClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ac := &AppContext{}

	var started atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- ac.RunLeaderElection(ctx, fakeClient, cfg, func() { started.Store(true) }, nil)
	}()

	require.Eventually(t, started.Load, 2*time.Second, 10*time.Millisecond, "expected to become leader")

	lease, err := fakeClient.CoordinationV1().Leases("custom-ns").Get(context.Background(), cfg.LeaderElectionLockName, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, lease.Spec.HolderIdentity)
	assert.Equal(t, cfg.LeaderElectionID, *lease.Spec.HolderIdentity)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLeaderElection did not return after context cancellation")
	}
}

func TestRunLeaderElection_DefaultsNamespaceWhenEmpty(t *testing.T) {
	cfg := fastLeaderElectionConfig()
	cfg.LeaderElectionID = "identity-default-ns"
	cfg.LeaderElectionNamespace = ""

	fakeClient := fake.NewClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ac := &AppContext{}

	var started atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- ac.RunLeaderElection(ctx, fakeClient, cfg, func() { started.Store(true) }, nil)
	}()

	require.Eventually(t, started.Load, 2*time.Second, 10*time.Millisecond, "expected to become leader")

	_, err := fakeClient.CoordinationV1().Leases("default").Get(context.Background(), cfg.LeaderElectionLockName, metav1.GetOptions{})
	assert.NoError(t, err, "expected lease to be created in the default namespace")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLeaderElection did not return after context cancellation")
	}
}

func TestRunLeaderElection_GeneratesIdentityWhenNotConfigured(t *testing.T) {
	cfg := fastLeaderElectionConfig()
	cfg.LeaderElectionID = ""

	fakeClient := fake.NewClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ac := &AppContext{}

	var started atomic.Bool
	done := make(chan error, 1)
	go func() {
		done <- ac.RunLeaderElection(ctx, fakeClient, cfg, func() { started.Store(true) }, nil)
	}()

	require.Eventually(t, started.Load, 2*time.Second, 10*time.Millisecond, "expected to become leader")

	lease, err := fakeClient.CoordinationV1().Leases("default").Get(context.Background(), cfg.LeaderElectionLockName, metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, lease.Spec.HolderIdentity)
	assert.NotEmpty(t, *lease.Spec.HolderIdentity)
	// generated identity is "<hostname>-<uuid>"
	assert.True(t, strings.Contains(*lease.Spec.HolderIdentity, "-"), "expected generated identity to contain a hostname/uuid separator")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLeaderElection did not return after context cancellation")
	}
}

func TestRunLeaderElection_NilCallbacksDoNotPanic(t *testing.T) {
	cfg := fastLeaderElectionConfig()
	cfg.LeaderElectionID = "identity-nil-callbacks"

	fakeClient := fake.NewClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ac := &AppContext{}

	done := make(chan error, 1)
	go func() {
		done <- ac.RunLeaderElection(ctx, fakeClient, cfg, nil, nil)
	}()

	require.Eventually(t, func() bool {
		_, err := fakeClient.CoordinationV1().Leases("default").Get(context.Background(), cfg.LeaderElectionLockName, metav1.GetOptions{})
		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "expected lease to be created")

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunLeaderElection did not return after context cancellation")
	}
}
