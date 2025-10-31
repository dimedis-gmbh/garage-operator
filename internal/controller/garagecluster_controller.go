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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	garageImage     = "dxflrs/garage:v1.0.0"
)

// GarageClusterReconciler reconciles a GarageCluster object
type GarageClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=garage.deuxfleurs.fr,resources=garageclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=garage.deuxfleurs.fr,resources=garageclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=garage.deuxfleurs.fr,resources=garageclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
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

	// Reconcile ConfigMap
	if err := r.reconcileConfigMap(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap")
		r.updateStatus(ctx, garageCluster, "Failed", "ConfigMap reconciliation failed: "+err.Error())
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, garageCluster); err != nil {
		logger.Error(err, "Failed to reconcile Service")
		r.updateStatus(ctx, garageCluster, "Failed", "Service reconciliation failed: "+err.Error())
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
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	garageCluster.Status.ReadyReplicas = sts.Status.ReadyReplicas

	if sts.Status.ReadyReplicas < garageCluster.Spec.ReplicaCount {
		msg := fmt.Sprintf("Waiting for replicas: %d/%d ready",
			sts.Status.ReadyReplicas, garageCluster.Spec.ReplicaCount)
		r.updateStatus(ctx, garageCluster, "Deploying", msg)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Configure layout if not done yet
	if garageCluster.Status.LayoutVersion == 0 {
		r.updateStatus(ctx, garageCluster, "LayoutConfiguring", "Configuring cluster layout")

		// Create LayoutManager
		config := ctrl.GetConfigOrDie()
		layoutMgr, err := NewLayoutManager(config)
		if err != nil {
			logger.Error(err, "Failed to create LayoutManager")
			r.updateStatus(ctx, garageCluster, "Failed", "LayoutManager creation failed: "+err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}

		if err := layoutMgr.ConfigureLayout(ctx, garageCluster); err != nil {
			logger.Error(err, "Failed to configure layout")
			r.updateStatus(ctx, garageCluster, "Failed", "Layout configuration failed: "+err.Error())
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}

		garageCluster.Status.LayoutVersion = 1
		if err := r.Status().Update(ctx, garageCluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status to Ready
	garageCluster.Status.S3Endpoint = fmt.Sprintf("http://%s.%s.svc.cluster.local:3900",
		garageCluster.Name, garageCluster.Namespace)
	r.updateStatus(ctx, garageCluster, "Ready", "Cluster is operational")

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func (r *GarageClusterReconciler) reconcileConfigMap(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	rpcSecret := gc.Spec.RPCSecret
	if rpcSecret == "" {
		rpcSecret = generateSecret(32)
	}

	region := "garage"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.Region != "" {
		region = gc.Spec.S3Api.Region
	}

	rootDomain := ".s3.garage.localhost"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.RootDomain != "" {
		rootDomain = gc.Spec.S3Api.RootDomain
	}

	configData := fmt.Sprintf(`metadata_dir = "/mnt/meta"
data_dir = "/mnt/data"
db_engine = "sqlite"
replication_mode = "%s"

rpc_bind_addr = "[::]:3901"
rpc_public_addr = "{{ env "GARAGE_RPC_PUBLIC_ADDR" }}:3901"
rpc_secret = "%s"

[kubernetes_discovery]
service_name = "%s"
namespace = "%s"

[s3_api]
s3_region = "%s"
api_bind_addr = "[::]:3900"
root_domain = "%s"

[admin]
api_bind_addr = "[::]:3903"
`, gc.Spec.ReplicationMode, rpcSecret, gc.Name, gc.Namespace, region, rootDomain)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name + "-config",
			Namespace: gc.Namespace,
		},
		Data: map[string]string{
			"garage.toml": configData,
		},
	}

	if err := controllerutil.SetControllerReference(gc, cm, r.Scheme); err != nil {
		return err
	}

	found := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	found.Data = cm.Data
	return r.Update(ctx, found)
}

func (r *GarageClusterReconciler) reconcileService(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name,
			Namespace: gc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None", // Headless service for StatefulSet
			Selector: map[string]string{
				"app": gc.Name,
			},
			Ports: []corev1.ServicePort{
				{Name: "s3-api", Port: 3900},
				{Name: "rpc", Port: 3901},
				{Name: "admin", Port: 3903},
			},
		},
	}

	if err := controllerutil.SetControllerReference(gc, svc, r.Scheme); err != nil {
		return err
	}

	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, svc)
	}
	return err
}

func (r *GarageClusterReconciler) reconcileStatefulSet(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	volumeSize, err := resource.ParseQuantity(gc.Spec.VolumeSize)
	if err != nil {
		return fmt.Errorf("invalid volume size: %w", err)
	}

	// Default resources if not specified
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}
	if gc.Spec.Resources != nil {
		resources = *gc.Spec.Resources
	}

	storageClassName := gc.Spec.StorageClass
	if storageClassName == "" {
		storageClassName = "" // Use cluster default
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name,
			Namespace: gc.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: gc.Name,
			Replicas:    &gc.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": gc.Name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": gc.Name},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{"app": gc.Name},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "garage",
							Image: garageImage,
							Env: []corev1.EnvVar{
								{
									Name: "GARAGE_RPC_PUBLIC_ADDR",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
							},
							Ports: []corev1.ContainerPort{
								{Name: "s3-api", ContainerPort: 3900},
								{Name: "rpc", ContainerPort: 3901},
								{Name: "admin", ContainerPort: 3903},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "meta", MountPath: "/mnt/meta"},
								{Name: "data", MountPath: "/mnt/data"},
								{Name: "config", MountPath: "/etc/garage.toml", SubPath: "garage.toml"},
							},
							Resources: resources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: gc.Name + "-config",
									},
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "meta"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: volumeSize,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: volumeSize,
							},
						},
					},
				},
			},
		},
	}

	// Only set storageClassName if explicitly provided
	if storageClassName != "" {
		for i := range sts.Spec.VolumeClaimTemplates {
			sts.Spec.VolumeClaimTemplates[i].Spec.StorageClassName = &storageClassName
		}
	}

	if err := controllerutil.SetControllerReference(gc, sts, r.Scheme); err != nil {
		return err
	}

	found := &appsv1.StatefulSet{}
	err = r.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, sts)
	} else if err != nil {
		return err
	}

	// Update only if there are changes (simplified check)
	if found.Spec.Replicas == nil || *found.Spec.Replicas != gc.Spec.ReplicaCount {
		found.Spec.Replicas = &gc.Spec.ReplicaCount
		return r.Update(ctx, found)
	}

	return nil
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

func (r *GarageClusterReconciler) updateStatus(ctx context.Context, gc *garagev1alpha1.GarageCluster, phase, message string) {
	gc.Status.Phase = phase

	// Update conditions
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               phase,
		Status:             metav1.ConditionTrue,
		LastTransitionTime: now,
		Reason:             phase,
		Message:            message,
	}

	// Simple condition management - in production use meta.SetStatusCondition
	found := false
	for i, cond := range gc.Status.Conditions {
		if cond.Type == phase {
			gc.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		gc.Status.Conditions = append(gc.Status.Conditions, condition)
	}

	r.Status().Update(ctx, gc)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GarageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&garagev1alpha1.GarageCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

func generateSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
