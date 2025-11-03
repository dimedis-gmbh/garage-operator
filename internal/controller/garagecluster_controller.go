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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

const (
	garageFinalizer = "garage.deuxfleurs.fr/finalizer"
)

// GarageClusterReconciler reconciles a GarageCluster object
type GarageClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garageclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garageclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garageclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=deuxfleurs.fr,resources=garagenodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create

func (r *GarageClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the GarageCluster instance
	garageCluster := &garagev1alpha1.GarageCluster{}
	if err := r.Get(ctx, req.NamespacedName, garageCluster); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get GarageCluster")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !garageCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, garageCluster)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(garageCluster, garageFinalizer) {
		controllerutil.AddFinalizer(garageCluster, garageFinalizer)
		if err := r.Update(ctx, garageCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile Secret
	if err := r.reconcileSecret(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile Secret")
		r.updateStatus(ctx, garageCluster, "Failed", "Secret reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile ConfigMap
	if err := r.reconcileConfigMap(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		r.updateStatus(ctx, garageCluster, "Failed", "ConfigMap reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile ServiceAccount and RBAC
	if err := r.reconcileServiceAccount(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile ServiceAccount")
		r.updateStatus(ctx, garageCluster, "Failed", "ServiceAccount reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		r.updateStatus(ctx, garageCluster, "Failed", "Service reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile Ingress
	if err := r.reconcileIngress(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile Ingress")
		r.updateStatus(ctx, garageCluster, "Failed", "Ingress reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile StatefulSet
	if err := r.reconcileStatefulSet(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile StatefulSet")
		r.updateStatus(ctx, garageCluster, "Failed", "StatefulSet reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Check if StatefulSet is ready
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      garageCluster.Name,
		Namespace: garageCluster.Namespace,
	}, sts); err != nil {
		if errors.IsNotFound(err) {
			// StatefulSet not yet created, will be reconciled again
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	garageCluster.Status.ReadyReplicas = sts.Status.ReadyReplicas
	podsReady := sts.Status.ReadyReplicas >= garageCluster.Spec.ReplicaCount

	if !podsReady {
		msg := fmt.Sprintf("Waiting for replicas: %d/%d ready",
			sts.Status.ReadyReplicas, garageCluster.Spec.ReplicaCount)
		r.updateStatus(ctx, garageCluster, "Deploying", msg)
		r.updateConditions(ctx, garageCluster, nil, nil, false)
		r.Status().Update(ctx, garageCluster)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Get REST config for kubectl exec
	config, err := ctrl.GetConfig()
	if err != nil {
		logger.Error(err, "Failed to get REST config")
		r.updateStatus(ctx, garageCluster, "Failed", "Failed to get REST config")
		r.updateConditions(ctx, garageCluster, nil, nil, podsReady)
		r.Status().Update(ctx, garageCluster)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Configure layout if not done yet
	if garageCluster.Status.LayoutVersion == 0 {
		// Determine the layout mode to use
		layoutMode := "zonePerNode" // default
		if garageCluster.Spec.Layout != nil && garageCluster.Spec.Layout.Mode != "" {
			layoutMode = garageCluster.Spec.Layout.Mode
		}

		r.updateStatus(ctx, garageCluster, "LayoutConfiguring", "Configuring cluster layout")
		r.Status().Update(ctx, garageCluster)

		// Create LayoutManager
		layoutMgr, err := NewLayoutManager(config)
		if err != nil {
			logger.Error(err, "Failed to create LayoutManager")
			r.updateStatus(ctx, garageCluster, "Failed", "LayoutManager creation failed: "+err.Error())
			r.Status().Update(ctx, garageCluster)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		if err := layoutMgr.ConfigureLayout(ctx, garageCluster); err != nil {
			logger.Error(err, "Failed to configure layout")
			r.updateStatus(ctx, garageCluster, "Failed", "Layout configuration failed: "+err.Error())
			r.Status().Update(ctx, garageCluster)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}

		// Mark layout as configured and save the applied mode
		garageCluster.Status.LayoutVersion = 1
		garageCluster.Status.AppliedLayoutMode = layoutMode
		if err := r.Status().Update(ctx, garageCluster); err != nil {
			return ctrl.Result{}, err
		}

		logger.Info("Layout configuration completed successfully", "mode", layoutMode)
	} else {
		// Layout already configured - validate that mode hasn't changed
		currentMode := "zonePerNode" // default
		if garageCluster.Spec.Layout != nil && garageCluster.Spec.Layout.Mode != "" {
			currentMode = garageCluster.Spec.Layout.Mode
		}

		appliedMode := garageCluster.Status.AppliedLayoutMode
		if appliedMode == "" {
			// For backwards compatibility with clusters created before this feature
			appliedMode = "zonePerNode"
			garageCluster.Status.AppliedLayoutMode = appliedMode
			r.Status().Update(ctx, garageCluster)
		}

		if currentMode != appliedMode {
			errorMsg := fmt.Sprintf("Layout mode cannot be changed after initial configuration. Applied mode: %s, requested mode: %s", appliedMode, currentMode)
			logger.Error(nil, errorMsg)
			r.updateStatus(ctx, garageCluster, "Failed", errorMsg)
			r.Status().Update(ctx, garageCluster)
			return ctrl.Result{}, fmt.Errorf("layout mode cannot be changed: applied=%s, requested=%s", appliedMode, currentMode)
		}

		// Check if cluster has been scaled up and needs layout update
		if podsReady {
			// Create LayoutManager
			layoutMgr, err := NewLayoutManager(config)
			if err != nil {
				logger.Error(err, "Failed to create LayoutManager for update")
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}

			// Try to update layout for new nodes (if any)
			if err := layoutMgr.UpdateLayout(ctx, garageCluster); err != nil {
				logger.Error(err, "Failed to update layout for scaled cluster")
				r.updateStatus(ctx, garageCluster, "Failed", "Layout update failed: "+err.Error())
				r.Status().Update(ctx, garageCluster)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	}

	// Pods are ready and layout is configured, now check cluster health

	health, status, err := r.checkClusterHealth(ctx, garageCluster, config)
	if err != nil {
		logger.Error(err, "Failed to check cluster health")
		// Don't fail completely - set conditions to Unknown and retry
		r.updateStatus(ctx, garageCluster, "Deploying", fmt.Sprintf("Waiting for cluster health: %v", err))
		r.updateConditions(ctx, garageCluster, nil, nil, podsReady)
		r.Status().Update(ctx, garageCluster)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Update S3 endpoint
	garageCluster.Status.S3Endpoint = fmt.Sprintf("http://%s.%s.svc.cluster.local:3900",
		garageCluster.Name, garageCluster.Namespace)

	// Update conditions based on health
	r.updateConditions(ctx, garageCluster, health, status, podsReady)

	// Set phase based on overall cluster health
	// healthy or degraded = Ready (operational)
	// unavailable = Deploying (not yet operational)
	if (health.Status == "healthy" || health.Status == "degraded") && podsReady {
		r.updateStatus(ctx, garageCluster, "Ready", fmt.Sprintf("Cluster is operational (%s)", health.Status))
	} else if health.Status == "unavailable" {
		r.updateStatus(ctx, garageCluster, "Deploying", "Cluster is unavailable, waiting for quorum")
	} else {
		r.updateStatus(ctx, garageCluster, "Deploying", fmt.Sprintf("Cluster status: %s", health.Status))
	}

	r.Status().Update(ctx, garageCluster)

	// Requeue to continuously check health
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *GarageClusterReconciler) reconcileDelete(ctx context.Context, gc *garagev1alpha1.GarageCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(gc, garageFinalizer) {
		logger.Info("Cleaning up GarageCluster resources")

		// Cleanup logic here if needed
		// For example: backup data, notify external systems, etc.

		controllerutil.RemoveFinalizer(gc, garageFinalizer)
		if err := r.Update(ctx, gc); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GarageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&garagev1alpha1.GarageCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
