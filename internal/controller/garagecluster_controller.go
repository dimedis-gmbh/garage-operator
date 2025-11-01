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
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
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
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch
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

	// Pods are ready, now check cluster health
	// Get REST config for kubectl exec
	config, err := ctrl.GetConfig()
	if err != nil {
		logger.Error(err, "Failed to get REST config")
		r.updateStatus(ctx, garageCluster, "Failed", "Failed to get REST config")
		r.updateConditions(ctx, garageCluster, nil, nil, podsReady)
		r.Status().Update(ctx, garageCluster)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

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

func (r *GarageClusterReconciler) reconcileSecret(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	secretName := gc.Name + "-rpc-secret"
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: gc.Namespace}, secret)

	if err != nil && errors.IsNotFound(err) {
		// Create new secret with generated RPC secret
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: gc.Namespace,
			},
			StringData: map[string]string{
				"rpc-secret": generateSecret(32),
			},
		}

		if err := controllerutil.SetControllerReference(gc, secret, r.Scheme); err != nil {
			return err
		}

		return r.Create(ctx, secret)
	}

	return err
}

func (r *GarageClusterReconciler) reconcileConfigMap(ctx context.Context, gc *garagev1alpha1.GarageCluster) error {
	region := "garage"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.Region != "" {
		region = gc.Spec.S3Api.Region
	}

	rootDomain := ".s3.garage.localhost"
	if gc.Spec.S3Api != nil && gc.Spec.S3Api.RootDomain != "" {
		rootDomain = gc.Spec.S3Api.RootDomain
	}

	// Default values matching Garage defaults
	replicationFactor := int32(3)
	if gc.Spec.ReplicationFactor > 0 {
		replicationFactor = gc.Spec.ReplicationFactor
	}

	consistencyMode := "consistent"
	if gc.Spec.ConsistencyMode != "" {
		consistencyMode = gc.Spec.ConsistencyMode
	}

	configData := fmt.Sprintf(`metadata_dir = "/mnt/meta"
data_dir = "/mnt/data"
db_engine = "sqlite"
replication_factor = %d
consistency_mode = "%s"

rpc_bind_addr = "[::]:3901"
rpc_secret_file = "/etc/garage/rpc-secret"

bootstrap_peers = []

[kubernetes_discovery]
service_name = "%s"
namespace = "%s"
skip_crd = false

[s3_api]
s3_region = "%s"
api_bind_addr = "[::]:3900"
root_domain = "%s"

[admin]
api_bind_addr = "[::]:3903"
`, replicationFactor, consistencyMode, gc.Name, gc.Namespace, region, rootDomain)

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

	// Update if changed
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
				{
					Name:       "s3-api",
					Port:       3900,
					TargetPort: intstr.FromInt(3900),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "rpc",
					Port:       3901,
					TargetPort: intstr.FromInt(3901),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "admin",
					Port:       3903,
					TargetPort: intstr.FromInt(3903),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			PublishNotReadyAddresses: true,
			// Do not set SessionAffinity for headless services
		},
	}

	if err := controllerutil.SetControllerReference(gc, svc, r.Scheme); err != nil {
		return err
	}

	found := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		log.FromContext(ctx).Info("Creating new Service")
		return r.Create(ctx, svc)
	} else if err == nil {
		// Only update if there are actual changes to avoid unnecessary API calls
		needsUpdate := false
		if !reflect.DeepEqual(found.Spec.Selector, svc.Spec.Selector) {
			found.Spec.Selector = svc.Spec.Selector
			needsUpdate = true
		}
		if !reflect.DeepEqual(found.Spec.Ports, svc.Spec.Ports) {
			found.Spec.Ports = svc.Spec.Ports
			needsUpdate = true
		}
		if found.Spec.PublishNotReadyAddresses != svc.Spec.PublishNotReadyAddresses {
			found.Spec.PublishNotReadyAddresses = svc.Spec.PublishNotReadyAddresses
			needsUpdate = true
		}
		if needsUpdate {
			log.FromContext(ctx).Info("Updating Service due to spec changes")
			return r.Update(ctx, found)
		}
		return nil
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

	// Create ClusterRole for Garage Kubernetes Discovery (CRD management)
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: gc.Namespace + "-" + gc.Name + "-garage-discovery",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"apiextensions.k8s.io"},
				Resources: []string{"customresourcedefinitions"},
				Verbs:     []string{"get", "list", "watch", "create", "patch"},
			},
			{
				APIGroups: []string{"deuxfleurs.fr"},
				Resources: []string{"garagenodes"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}

	foundClusterRole := &rbacv1.ClusterRole{}
	err = r.Get(ctx, types.NamespacedName{Name: clusterRole.Name}, foundClusterRole)
	if err != nil && errors.IsNotFound(err) {
		if err := r.Create(ctx, clusterRole); err != nil {
			return err
		}
	}

	// Create ClusterRoleBinding
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: gc.Namespace + "-" + gc.Name + "-garage-discovery",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRole.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.Name,
				Namespace: gc.Namespace,
			},
		},
	}

	foundClusterRB := &rbacv1.ClusterRoleBinding{}
	err = r.Get(ctx, types.NamespacedName{Name: clusterRoleBinding.Name}, foundClusterRB)
	if err != nil && errors.IsNotFound(err) {
		if err := r.Create(ctx, clusterRoleBinding); err != nil {
			return err
		}
	}

	// Create Role for endpoints (namespace-scoped)
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
								{
									Name:      "rpc-secret",
									MountPath: "/etc/garage",
									ReadOnly:  true,
								},
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
						{
							Name: "rpc-secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  gc.Name + "-rpc-secret",
									DefaultMode: func() *int32 { mode := int32(0400); return &mode }(),
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
	r.Status().Update(ctx, gc)
}

// ClusterHealth represents Garage cluster health information
type ClusterHealth struct {
	Status           string `json:"status"`
	KnownNodes       int    `json:"knownNodes"`
	ConnectedNodes   int    `json:"connectedNodes"`
	StorageNodes     int    `json:"storageNodes"`
	StorageNodesUp   int    `json:"storageNodesUp"`
	Partitions       int    `json:"partitions"`
	PartitionsQuorum int    `json:"partitionsQuorum"`
	PartitionsAllOk  int    `json:"partitionsAllOk"`
}

// ClusterStatus represents Garage cluster status information
type ClusterStatus struct {
	LayoutVersion int64 `json:"layoutVersion"`
}

// checkClusterHealth queries the Garage cluster health via kubectl exec
func (r *GarageClusterReconciler) checkClusterHealth(ctx context.Context, gc *garagev1alpha1.GarageCluster, config *rest.Config) (*ClusterHealth, *ClusterStatus, error) {
	logger := log.FromContext(ctx)

	// Get the first ready pod to query
	podName := fmt.Sprintf("%s-0", gc.Name)

	// Query cluster health
	healthCmd := []string{"./garage", "json-api", "GetClusterHealth"}
	healthOutput, err := r.execInPod(ctx, gc.Namespace, podName, healthCmd, config)
	if err != nil {
		logger.Error(err, "Failed to execute GetClusterHealth")
		return nil, nil, fmt.Errorf("failed to query cluster health: %w", err)
	}

	health := &ClusterHealth{}
	if err := json.Unmarshal([]byte(healthOutput), health); err != nil {
		logger.Error(err, "Failed to parse cluster health response", "output", healthOutput)
		return nil, nil, fmt.Errorf("failed to parse health response: %w", err)
	}

	// Query cluster status for layout version
	statusCmd := []string{"./garage", "json-api", "GetClusterStatus"}
	statusOutput, err := r.execInPod(ctx, gc.Namespace, podName, statusCmd, config)
	if err != nil {
		logger.Error(err, "Failed to execute GetClusterStatus")
		return health, nil, fmt.Errorf("failed to query cluster status: %w", err)
	}

	status := &ClusterStatus{}
	if err := json.Unmarshal([]byte(statusOutput), status); err != nil {
		logger.Error(err, "Failed to parse cluster status response", "output", statusOutput)
		return health, nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return health, status, nil
}

// execInPod executes a command in a pod and returns the output
func (r *GarageClusterReconciler) execInPod(ctx context.Context, namespace, podName string, command []string, config *rest.Config) (string, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}, runtime.NewParameterCodec(r.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr string
	stdoutWriter := &stringWriter{str: &stdout}
	stderrWriter := &stringWriter{str: &stderr}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdoutWriter,
		Stderr: stderrWriter,
	})

	if err != nil {
		return "", fmt.Errorf("exec failed: %w (stderr: %s)", err, stderr)
	}

	return stdout, nil
}

// stringWriter implements io.Writer to capture output as string
type stringWriter struct {
	str *string
}

func (sw *stringWriter) Write(p []byte) (n int, err error) {
	*sw.str += string(p)
	return len(p), nil
}

// updateConditions updates the Kubernetes Conditions based on cluster health
func (r *GarageClusterReconciler) updateConditions(ctx context.Context, gc *garagev1alpha1.GarageCluster, health *ClusterHealth, status *ClusterStatus, podsReady bool) {
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
	isDegraded := health.Status == "degraded"
	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "Degraded",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[isDegraded]),
		Reason:  "ClusterDegradation",
		Message: fmt.Sprintf("Cluster is %s: %d/%d nodes, %d/%d partitions OK", health.Status, health.ConnectedNodes, health.StorageNodes, health.PartitionsAllOk, health.Partitions),
	})

	// Ready condition - cluster is ready if it can serve requests (healthy or degraded, but not unavailable)
	// A degraded cluster is still operational, just not fully optimal
	isOperational := health.Status == "healthy" || health.Status == "degraded"
	clusterReady := podsReady && layoutReady && isOperational

	var readyMessage string
	if health.Status == "healthy" {
		readyMessage = "Cluster is fully operational"
	} else if health.Status == "degraded" {
		readyMessage = fmt.Sprintf("Cluster is operational but degraded (%d/%d nodes connected)", health.ConnectedNodes, health.StorageNodes)
	} else {
		readyMessage = fmt.Sprintf("Cluster is unavailable: %s", health.Status)
	}

	meta.SetStatusCondition(&gc.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionStatus(map[bool]string{true: "True", false: "False"}[clusterReady]),
		Reason:  "ClusterHealth",
		Message: readyMessage,
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *GarageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&garagev1alpha1.GarageCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func generateSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)
}
