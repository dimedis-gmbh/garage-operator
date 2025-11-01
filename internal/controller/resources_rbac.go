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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

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
