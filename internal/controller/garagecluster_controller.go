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
	rbacv1 "k8s.io/api/rbac/v1"
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
	garageImage     = "dxflrs/garage:v2.1.0"
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
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
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
	// Generate and persist RPC secret if not set
	if gc.Spec.RPCSecret == "" {
		gc.Spec.RPCSecret = generateSecret(32)
		if err := r.Update(ctx, gc); err != nil {
			return err
		}
	}
	
	rpcSecret := gc.Spec.RPCSecret

	region := "garage"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.Region != "" {
		region = gc.Spec.S3Api.Region
	}

	rootDomain := ".s3.garage.localhost"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.RootDomain != "" {
		rootDomain = gc.Spec.S3Api.RootDomain
	}

	// Create a ConfigMap for each pod in the StatefulSet
	for i := int32(0); i < gc.Spec.ReplicaCount; i++ {
		podName := fmt.Sprintf("%s-%d", gc.Name, i)
		rpcPublicAddr := fmt.Sprintf("%s.%s.%s.svc.cluster.local:3901", podName, gc.Name, gc.Namespace)

		// Map old replication_mode to new replication_factor
		// "1" -> 1, "2" -> 2, "3" -> 3
		replicationFactor := gc.Spec.ReplicationMode

		configData := fmt.Sprintf(`metadata_dir = "/mnt/meta"
data_dir = "/mnt/data"
db_engine = "sqlite"
replication_factor = %s
consistency_mode = "consistent"

rpc_bind_addr = "[::]:3901"
rpc_public_addr = "%s"
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
`, replicationFactor, rpcPublicAddr, rpcSecret, gc.Name, gc.Namespace, region, rootDomain)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-config-%d", gc.Name, i),
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
			if err := r.Create(ctx, cm); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			found.Data = cm.Data
			if err := r.Update(ctx, found); err != nil {
				return err
			}
		}
	}

	return nil
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

func (r *GarageClusterReconciler) reconcileServiceAccount(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	// Create ServiceAccount
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name,
			Namespace: gc.Namespace,
		},
	}

	if err := controllerutil.SetControllerReference(gc, sa, r.Scheme); err != nil {
		return err
	}

	foundSA := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, foundSA)
	if err != nil && errors.IsNotFound(err) {
		if err := r.Create(ctx, sa); err != nil {
			return err
		}
	}

	// Create Role for Kubernetes discovery
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name + "-discovery",
			Namespace: gc.Namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"endpoints"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	if err := controllerutil.SetControllerReference(gc, role, r.Scheme); err != nil {
		return err
	}

	foundRole := &rbacv1.Role{}
	err = r.Get(ctx, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, foundRole)
	if err != nil && errors.IsNotFound(err) {
		if err := r.Create(ctx, role); err != nil {
			return err
		}
	}

	// Create RoleBinding
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.Name + "-discovery",
			Namespace: gc.Namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.Name,
				Namespace: gc.Namespace,
			},
		},
	}

	if err := controllerutil.SetControllerReference(gc, roleBinding, r.Scheme); err != nil {
		return err
	}

	foundRB := &rbacv1.RoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: roleBinding.Name, Namespace: roleBinding.Namespace}, foundRB)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, roleBinding)
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
					ServiceAccountName: gc.Name,
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
					InitContainers: []corev1.Container{
						{
							Name:  "config-init",
							Image: "busybox:latest",
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								`ORDINAL=$(echo $HOSTNAME | sed 's/.*-//'); cp /etc/garage-configs/config-$ORDINAL/garage.toml /etc/garage.toml`,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "config", MountPath: "/etc"},
								{Name: "config-templates", MountPath: "/etc/garage-configs"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "garage",
							Image:   garageImage,
							Command: []string{"/garage"},
							Args:    []string{"server"},
							Ports: []corev1.ContainerPort{
								{Name: "s3-api", ContainerPort: 3900},
								{Name: "rpc", ContainerPort: 3901},
								{Name: "admin", ContainerPort: 3903},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "meta", MountPath: "/mnt/meta"},
								{Name: "data", MountPath: "/mnt/data"},
								{
									Name:      "config",
									MountPath: "/etc/garage.toml",
									SubPath:   "garage.toml",
								},
							},
							Resources: resources,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "config-templates",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: func() []corev1.VolumeProjection {
										var sources []corev1.VolumeProjection
										for i := int32(0); i < gc.Spec.ReplicaCount; i++ {
											sources = append(sources, corev1.VolumeProjection{
												ConfigMap: &corev1.ConfigMapProjection{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: fmt.Sprintf("%s-config-%d", gc.Name, i),
													},
													Items: []corev1.KeyToPath{
														{
															Key:  "garage.toml",
															Path: fmt.Sprintf("config-%d/garage.toml", i),
														},
													},
												},
											})
										}
										return sources
									}(),
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
