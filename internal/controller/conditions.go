/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

func (r *GarageClusterReconciler) updateStatus(ctx context.Context, gc *garagev1alpha1.GarageCluster, phase string) {
	gc.Status.Phase = phase
	if err := r.Status().Update(ctx, gc); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update status")
	}
}

// updateConditions updates the Kubernetes Conditions based on cluster health
func (r *GarageClusterReconciler) updateConditions(_ context.Context, gc *garagev1alpha1.GarageCluster, health *ClusterHealth, status *ClusterStatus, podsReady bool) {
	// PodsReady condition
	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "PodsReady",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[podsReady]),
		Reason:  "PodStatus",
		Message: fmt.Sprintf("%d/%d pods ready", gc.Status.ReadyReplicas, gc.Spec.ReplicaCount),
	})

	if health == nil {
		// Can't check health - mark other conditions as Unknown
		meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
			Type:    "NodesConnected",
			Status:  metav1.ConditionUnknown,
			Reason:  "HealthCheckFailed",
			Message: "Unable to query cluster health",
		})
		meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
			Type:    "LayoutReady",
			Status:  metav1.ConditionUnknown,
			Reason:  "HealthCheckFailed",
			Message: "Unable to query cluster status",
		})
		meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "HealthCheckFailed",
			Message: "Cluster health check failed",
		})
		return
	}

	// Update status fields
	gc.Status.ConnectedNodes = int32(health.ConnectedNodes)
	if status != nil {
		gc.Status.LayoutVersion = status.LayoutVersion
	}

	// NodesConnected condition
	nodesConnected := health.ConnectedNodes >= health.StorageNodes
	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "NodesConnected",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[nodesConnected]),
		Reason:  "NodeConnectivity",
		Message: fmt.Sprintf("%d/%d nodes connected", health.ConnectedNodes, health.StorageNodes),
	})

	// LayoutReady condition (partitions have quorum)
	layoutReady := health.PartitionsQuorum == health.Partitions
	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "LayoutReady",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[layoutReady]),
		Reason:  "LayoutStatus",
		Message: fmt.Sprintf("%d/%d partitions have quorum (status: %s)", health.PartitionsQuorum, health.Partitions, health.Status),
	})

	// Degraded condition - cluster is degraded if not all nodes/partitions are optimal
	isDegraded := health.Status == healthStatusDegraded
	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "Degraded",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[isDegraded]),
		Reason:  "ClusterDegradation",
		Message: fmt.Sprintf("Cluster is %s: %d/%d nodes, %d/%d partitions OK", health.Status, health.ConnectedNodes, health.StorageNodes, health.PartitionsAllOk, health.Partitions),
	})

	// Ready condition - cluster is ready if it can serve requests (healthy or degraded, but not unavailable)
	// A degraded cluster is still operational, just not fully optimal
	isOperational := health.Status == healthStatusHealthy || health.Status == healthStatusDegraded
	clusterReady := podsReady && layoutReady && isOperational

	var readyMessage string
	switch health.Status {
	case healthStatusHealthy:
		readyMessage = "Cluster is fully operational"
	case healthStatusDegraded:
		readyMessage = fmt.Sprintf("Cluster is operational but degraded (%d/%d nodes connected)", health.ConnectedNodes, health.StorageNodes)
	default:
		readyMessage = fmt.Sprintf("Cluster is unavailable: %s", health.Status)
	}

	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[clusterReady]),
		Reason:  "ClusterHealth",
		Message: readyMessage,
	})
}
