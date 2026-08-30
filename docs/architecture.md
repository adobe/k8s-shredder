# Architecture

<p align="left"><img src="https://lucid.app/publicSegments/view/b6059d9c-d180-40b8-9e85-dd995c44b8cc/image.png" width="70%" /></p>

## Leader election flow

Before leader election was introduced, each controller replica could start its own scheduler loop and attempt to manage parked nodes independently. In a multi-replica deployment that meant duplicate eviction work and possible coordination issues when several instances tried to process the same resources.

The current implementation uses Kubernetes Lease-based leader election to ensure that only one replica actively runs the scheduler. The elected leader performs parking and eviction operations while the other replicas wait for leadership to change. If the leader exits or loses the lease, another replica can take over automatically. This keeps the behavior deterministic and avoids duplicate work in HA deployments.

The relevant configuration knobs are `EnableLeaderElection`, `LeaderElectionLockName`, `LeaderElectionNamespace`, `LeaderElectionID`, `LeaderElectionLeaseDuration`, `LeaderElectionRenewDeadline`, and `LeaderElectionRetryPeriod`.
