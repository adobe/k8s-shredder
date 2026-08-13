package utils

import (
	"context"
	"os"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// RunLeaderElection starts the client-go leader election loop using the
// configuration provided. It blocks until the provided context is done or
// leader election stops. Callbacks `onStartedLeading` and `onStoppedLeading`
// are invoked when leadership is acquired or lost respectively.
func (ac *AppContext) RunLeaderElection(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	cfg config.Config,
	onStartedLeading func(),
	onStoppedLeading func(),
) error {
	id := cfg.LeaderElectionID
	if id == "" {
		hn, err := os.Hostname()
		if err != nil {
			return err
		}
		id = hn + "-" + uuid.New().String()
	}

	lockName := cfg.LeaderElectionLockName
	lockNamespace := cfg.LeaderElectionNamespace
	if lockNamespace == "" {
		lockNamespace = "default"
	}

	// create the lock
	rl, err := resourcelock.New(resourcelock.LeasesResourceLock, lockNamespace, lockName,
		kubeClient.CoreV1(), kubeClient.CoordinationV1(), resourcelock.ResourceLockConfig{
			Identity: id,
		})
	if err != nil {
		return err
	}

	lec := leaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   cfg.LeaderElectionLeaseDuration,
		RenewDeadline:   cfg.LeaderElectionRenewDeadline,
		RetryPeriod:     cfg.LeaderElectionRetryPeriod,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Infof("Became leader (id=%s)", id)
				if onStartedLeading != nil {
					onStartedLeading()
				}
			},
			OnStoppedLeading: func() {
				log.Infof("Stopped being leader (id=%s)", id)
				if onStoppedLeading != nil {
					onStoppedLeading()
				}
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					// we are the leader
					return
				}
				log.Infof("New leader elected: %s", identity)
			},
		},
	}

	elector, err := leaderelection.NewLeaderElector(lec)
	if err != nil {
		return err
	}

	// Run blocks until ctx is done or leader election loop exits
	elector.Run(ctx)

	// give a tiny grace period to let callbacks finish
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
	}

	return nil
}
