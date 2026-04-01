/*
Copyright 2026.

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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	// DefaultNamespace is the namespace the operator creates on startup for
	// DHCP server resources. When CRs are deployed here the operator
	// automatically binds the kea-dhcp SCC to the pod ServiceAccount.
	DefaultNamespace = "kea-system"

	sccName        = "kea-dhcp"
	sccClusterRole = "system:openshift:scc:kea-dhcp"
)

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;create
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;create;update
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=kea-dhcp,verbs=use
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update

// EnsureDefaultNamespace creates the kea-system namespace if it doesn't exist.
func EnsureDefaultNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: DefaultNamespace,
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating namespace %s: %w", DefaultNamespace, err)
	}
	return nil
}

// EnsureSCC creates the kea-dhcp SecurityContextConstraints on OpenShift.
// Must only be called when the cluster is detected as OpenShift.
func EnsureSCC(ctx context.Context, c client.Client) error {
	scc := &unstructured.Unstructured{}
	scc.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "security.openshift.io",
		Version: "v1",
		Kind:    "SecurityContextConstraints",
	})

	err := c.Get(ctx, client.ObjectKey{Name: sccName}, scc)
	if err == nil {
		return nil // already exists
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("checking SCC %s: %w", sccName, err)
	}

	scc.Object = map[string]interface{}{
		"apiVersion": "security.openshift.io/v1",
		"kind":       "SecurityContextConstraints",
		"metadata": map[string]interface{}{
			"name": sccName,
			"annotations": map[string]interface{}{
				"kubernetes.io/description": "Minimal SCC for Kea DHCP server pods. Grants NET_RAW and NET_BIND_SERVICE " +
					"(required for raw DHCP sockets and binding to ports < 1024). " +
					"Optionally allows NET_ADMIN for NAD-attached interfaces.",
			},
		},
		"allowPrivilegedContainer": false,
		"allowPrivilegeEscalation": true,
		"allowHostDirVolumePlugin": false,
		"allowHostIPC":             false,
		"allowHostNetwork":         false,
		"allowHostPID":             false,
		"allowHostPorts":           false,
		"readOnlyRootFilesystem":   false,
		"requiredDropCapabilities": []interface{}{"ALL"},
		"allowedCapabilities":      []interface{}{"NET_RAW", "NET_BIND_SERVICE", "NET_ADMIN"},
		"defaultAddCapabilities":   []interface{}{},
		"runAsUser":                map[string]interface{}{"type": "RunAsAny"},
		"seLinuxContext":           map[string]interface{}{"type": "MustRunAs"},
		"fsGroup":                  map[string]interface{}{"type": "MustRunAs"},
		"supplementalGroups":       map[string]interface{}{"type": "RunAsAny"},
		"volumes":                  []interface{}{"configMap", "downwardAPI", "emptyDir", "projected", "secret"},
	}

	if err := c.Create(ctx, scc); err != nil {
		return fmt.Errorf("creating SCC %s: %w", sccName, err)
	}
	return nil
}

// EnsureSCCClusterRole creates the ClusterRole that grants "use" on the
// kea-dhcp SCC. OpenShift auto-generates these for built-in SCCs but not
// always for custom ones, so we create it ourselves to be safe.
func EnsureSCCClusterRole(ctx context.Context, c client.Client) error {
	cr := &rbacv1.ClusterRole{}
	if err := c.Get(ctx, client.ObjectKey{Name: sccClusterRole}, cr); err == nil {
		return nil // already exists
	}

	cr = &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: sccClusterRole,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kea-dhcp-operator",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{sccName},
				Verbs:         []string{"use"},
			},
		},
	}
	if err := c.Create(ctx, cr); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating ClusterRole %s: %w", sccClusterRole, err)
	}
	return nil
}

// ensureSCCRoleBinding creates a RoleBinding in the given namespace that grants
// the service account the kea-dhcp SCC via the ClusterRole
// system:openshift:scc:kea-dhcp.
func ensureSCCRoleBinding(ctx context.Context, c client.Client, namespace, crName string) error {
	saName := resources.ServiceAccountName(crName)
	rbName := fmt.Sprintf("kea-dhcp-scc-%s", saName)

	existing := &rbacv1.RoleBinding{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: rbName}, existing); err == nil {
		return nil // already exists
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rbName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "kea-dhcp-operator",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     sccClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: namespace,
			},
		},
	}
	if err := c.Create(ctx, rb); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("creating SCC RoleBinding for %s/%s: %w", namespace, saName, err)
	}
	return nil
}
