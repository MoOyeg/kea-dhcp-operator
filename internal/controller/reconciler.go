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
	"strings"
	"time"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// reconcileResource creates or updates a Kubernetes resource using the
// create-or-update pattern. It sets the controller reference on the object
// so that owned resources are garbage-collected when the parent is deleted.
// The mutate function applies the full desired spec so that updates to the
// CR spec are reflected in the owned resources.
func reconcileResource(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object, obj client.Object) error {
	logger := log.FromContext(ctx)

	if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Capture the desired state before CreateOrUpdate overwrites obj with the
	// live state from the API server.
	mutateFn := buildMutateFn(owner, obj, scheme)

	key := client.ObjectKeyFromObject(obj)

	result, err := controllerutil.CreateOrUpdate(ctx, c, obj, mutateFn)
	if err != nil {
		return fmt.Errorf("reconciling %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, key, err)
	}

	switch result {
	case controllerutil.OperationResultCreated:
		logger.Info("created resource", "kind", fmt.Sprintf("%T", obj), "name", key.Name)
	case controllerutil.OperationResultUpdated:
		logger.Info("updated resource", "kind", fmt.Sprintf("%T", obj), "name", key.Name)
	default:
		logger.V(1).Info("resource unchanged", "kind", fmt.Sprintf("%T", obj), "name", key.Name)
	}

	return nil
}

// buildMutateFn captures the desired state of obj and returns a mutate function
// that applies that state back after CreateOrUpdate fetches the live object.
func buildMutateFn(owner metav1.Object, obj client.Object, scheme *runtime.Scheme) controllerutil.MutateFn {
	switch desired := obj.(type) {
	case *corev1.ConfigMap:
		desiredData := desired.Data
		desiredLabels := desired.Labels
		return func() error {
			desired.Data = desiredData
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *appsv1.Deployment:
		desiredSpec := *desired.Spec.DeepCopy()
		desiredLabels := desired.Labels
		desiredAnnotations := operatorAnnotations(desired.Annotations)
		return func() error {
			desired.Spec = desiredSpec
			desired.Labels = desiredLabels
			mergeOperatorAnnotations(desired, desiredAnnotations)
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *appsv1.StatefulSet:
		desiredSpec := *desired.Spec.DeepCopy()
		desiredLabels := desired.Labels
		desiredAnnotations := operatorAnnotations(desired.Annotations)
		return func() error {
			desired.Spec = desiredSpec
			desired.Labels = desiredLabels
			mergeOperatorAnnotations(desired, desiredAnnotations)
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *corev1.Service:
		desiredSpec := *desired.Spec.DeepCopy()
		desiredLabels := desired.Labels
		return func() error {
			// Preserve ClusterIP assigned by the API server.
			clusterIP := desired.Spec.ClusterIP
			clusterIPs := desired.Spec.ClusterIPs
			desired.Spec = desiredSpec
			if clusterIP != "" {
				desired.Spec.ClusterIP = clusterIP
			}
			if len(clusterIPs) > 0 {
				desired.Spec.ClusterIPs = clusterIPs
			}
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *corev1.Secret:
		desiredData := desired.Data
		desiredLabels := desired.Labels
		return func() error {
			desired.Data = desiredData
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *corev1.ServiceAccount:
		desiredLabels := desired.Labels
		return func() error {
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *rbacv1.RoleBinding:
		desiredSubjects := desired.Subjects
		desiredRoleRef := desired.RoleRef
		desiredLabels := desired.Labels
		return func() error {
			desired.Subjects = desiredSubjects
			desired.RoleRef = desiredRoleRef
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	case *monitoringv1.PodMonitor:
		desiredSpec := *desired.Spec.DeepCopy()
		desiredLabels := desired.Labels
		return func() error {
			desired.Spec = desiredSpec
			desired.Labels = desiredLabels
			return controllerutil.SetControllerReference(owner, desired, scheme)
		}

	default:
		// Fallback: just set controller reference (no spec update).
		return func() error {
			return controllerutil.SetControllerReference(owner, obj, scheme)
		}
	}
}

// setCondition updates or appends a condition in the given conditions slice.
func setCondition(conditions *[]keav1alpha1.ConditionStatus, condType, status, reason, message string) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i, c := range *conditions {
		if c.Type == condType {
			if c.Status != status {
				(*conditions)[i].LastTransitionTime = now
			}
			(*conditions)[i].Status = status
			(*conditions)[i].Reason = reason
			(*conditions)[i].Message = message
			return
		}
	}
	*conditions = append(*conditions, keav1alpha1.ConditionStatus{
		Type:               condType,
		Status:             status,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

// resolveSecretValue reads a value from a Kubernetes Secret. The ref parameter
// identifies the Secret by name (via LocalObjectReference), and key identifies
// the data key within the Secret.
func resolveSecretValue(ctx context.Context, c client.Client, namespace string, ref *corev1.LocalObjectReference, key string) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is nil")
	}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		if errors.IsNotFound(err) {
			return "", fmt.Errorf("secret %q not found in namespace %q", ref.Name, namespace)
		}
		return "", fmt.Errorf("fetching secret %q: %w", ref.Name, err)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", key, ref.Name)
	}
	return string(val), nil
}

// dbCredentialEnvVars builds environment variables with ValueFrom.SecretKeyRef for
// database credentials, and returns the env var placeholder strings to use in
// the config. This avoids rendering actual secrets into the ConfigMap.
func dbCredentialEnvVars(prefix string, db *keav1alpha1.DatabaseConfig) (envVars []corev1.EnvVar, userPlaceholder, passwordPlaceholder string) {
	if db == nil || db.CredentialsSecretRef == nil {
		return nil, "", ""
	}
	userEnvName := prefix + "_USER"
	passEnvName := prefix + "_PASSWORD"
	envVars = []corev1.EnvVar{
		{
			Name: userEnvName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: *db.CredentialsSecretRef,
					Key:                  secretKeyUsername,
				},
			},
		},
		{
			Name: passEnvName,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: *db.CredentialsSecretRef,
					Key:                  secretKeyPassword,
				},
			},
		},
	}
	return envVars, "$" + userEnvName, "$" + passEnvName
}

// secretKeyRefEnvVar builds an environment variable with ValueFrom.SecretKeyRef
// and returns the placeholder string for use in config rendering.
func secretKeyRefEnvVar(envName string, sel *corev1.SecretKeySelector) (corev1.EnvVar, string) {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: sel,
		},
	}, "$" + envName
}

// StorkServerInfo holds the resolved server URL and token for stork-agent auto-registration.
type StorkServerInfo struct {
	// Name is the KeaStorkServer the agents report to. It labels the agent pods
	// so that server's reconciler can find them.
	Name            string
	ServerURL       string
	TokenSecretName string
}

// resolveStorkServer looks up a KeaStorkServer CR by reference or by scanning the
// namespace. It returns the server URL (Service DNS) and the token Secret name so
// stork-agents can register automatically.
func resolveStorkServer(ctx context.Context, c client.Client, namespace string, ref *corev1.LocalObjectReference) *StorkServerInfo {
	logger := log.FromContext(ctx)

	if ref != nil {
		// Explicit reference: look up the named KeaStorkServer.
		server := &keav1alpha1.KeaStorkServer{}
		key := types.NamespacedName{Name: ref.Name, Namespace: namespace}
		if err := c.Get(ctx, key, server); err != nil {
			logger.V(1).Info("referenced KeaStorkServer not found", "name", ref.Name, "error", err)
			return nil
		}
		return storkServerInfoFromCR(server)
	}

	// No explicit reference: scan the namespace for any KeaStorkServer.
	serverList := &keav1alpha1.KeaStorkServerList{}
	if err := c.List(ctx, serverList, client.InNamespace(namespace)); err != nil {
		logger.V(1).Info("failed to list KeaStorkServers", "error", err)
		return nil
	}
	if len(serverList.Items) == 0 {
		return nil
	}

	// Use the first KeaStorkServer found.
	return storkServerInfoFromCR(&serverList.Items[0])
}

// storkServerInfoFromCR extracts the server URL and token secret name from a KeaStorkServer CR.
func storkServerInfoFromCR(server *keav1alpha1.KeaStorkServer) *StorkServerInfo {
	port := resources.DefaultStorkServerPort
	if server.Spec.Port != nil {
		port = *server.Spec.Port
	}
	svcName := resources.ServiceName(server.Name, resources.StorkServerComponent)
	serverURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", svcName, server.Namespace, port)
	tokenSecretName := resources.StorkServerTokenSecretName(server.Name)

	return &StorkServerInfo{
		Name:            server.Name,
		ServerURL:       serverURL,
		TokenSecretName: tokenSecretName,
	}
}

// reconcileRoute creates or updates an OpenShift Route (unstructured). Since
// Routes are unstructured, we handle them separately from typed resources.
// The controller reference is set so the Route is garbage-collected with its owner.
func reconcileRoute(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner metav1.Object, route *unstructured.Unstructured) error {
	logger := log.FromContext(ctx)

	if err := controllerutil.SetControllerReference(owner, route, scheme); err != nil {
		return fmt.Errorf("setting controller reference on Route: %w", err)
	}

	// Capture the desired spec before CreateOrUpdate overwrites with live state.
	desiredSpec, _, _ := unstructured.NestedMap(route.Object, "spec")
	desiredLabels := route.GetLabels()

	key := client.ObjectKeyFromObject(route)

	result, err := controllerutil.CreateOrUpdate(ctx, c, route, func() error {
		if err := unstructured.SetNestedMap(route.Object, desiredSpec, "spec"); err != nil {
			return err
		}
		route.SetLabels(desiredLabels)
		return controllerutil.SetControllerReference(owner, route, scheme)
	})
	if err != nil {
		return fmt.Errorf("reconciling Route %s: %w", key, err)
	}

	switch result {
	case controllerutil.OperationResultCreated:
		logger.Info("created Route", "name", key.Name)
	case controllerutil.OperationResultUpdated:
		logger.Info("updated Route", "name", key.Name)
	}

	return nil
}

// enqueueStorkDependents returns a handler.MapFunc that lists CRs in the same
// namespace as the triggering KeaStorkServer and returns reconcile requests for
// those with Stork enabled. The listFn callback lists and filters the CRs.
// This eliminates duplication between enqueueDhcp4/6ServersInNamespace.
func enqueueStorkDependents(listFn func(ctx context.Context, c client.Reader, namespace string) []ctrl.Request, c client.Reader) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		return listFn(ctx, c, obj.GetNamespace())
	}
}

// enqueueOnStorkTokenSecret returns a handler.MapFunc that fires only for a
// KeaStorkServer's agent token Secret. When the Stork server controller writes
// the real token into it, the Stork-enabled CRs in the namespace re-resolve the
// token and roll their stork-agent sidecars.
func enqueueOnStorkTokenSecret(listFn func(ctx context.Context, c client.Reader, namespace string) []ctrl.Request, c client.Reader) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		labels := obj.GetLabels()
		if labels[resources.LabelComponent] != resources.StorkServerComponent ||
			obj.GetName() != resources.StorkServerTokenSecretName(labels[resources.LabelInstance]) {
			return nil
		}
		return listFn(ctx, c, obj.GetNamespace())
	}
}

// deleteStorkMonitoring removes the PodMonitor, Stork metrics Service and (on
// OpenShift) metrics Route that were created while the Stork agent was enabled.
// Only objects controlled by owner are deleted. A PodMonitor or Route kind that
// the cluster or scheme doesn't know is not an error.
func deleteStorkMonitoring(ctx context.Context, c client.Client, owner metav1.Object, component string, isOpenShift bool) error {
	ns, name := owner.GetNamespace(), owner.GetName()
	objs := []client.Object{
		&monitoringv1.PodMonitor{},
		&corev1.Service{},
	}
	names := []string{
		resources.PodMonitorName(name, component),
		resources.StorkMetricsServiceName(name, component),
	}
	if isOpenShift {
		route := &unstructured.Unstructured{}
		route.SetGroupVersionKind(resources.RouteGVK)
		objs = append(objs, route)
		names = append(names, resources.StorkRouteName(name, component))
	}

	for i, obj := range objs {
		if err := c.Get(ctx, types.NamespacedName{Name: names[i], Namespace: ns}, obj); err != nil {
			if errors.IsNotFound(err) || meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) {
				continue
			}
			return fmt.Errorf("fetching %s for Stork cleanup: %w", names[i], err)
		}
		if !metav1.IsControlledBy(obj, owner) {
			continue
		}
		if err := c.Delete(ctx, obj); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting %s: %w", names[i], err)
		}
		log.FromContext(ctx).Info("deleted Stork monitoring object after Stork was disabled", "name", names[i])
	}
	return nil
}

// listStorkEnabledDhcp4 lists KeaDhcp4Servers with Stork enabled in the given namespace.
func listStorkEnabledDhcp4(ctx context.Context, c client.Reader, namespace string) []ctrl.Request {
	serverList := &keav1alpha1.KeaDhcp4ServerList{}
	if err := c.List(ctx, serverList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	var requests []ctrl.Request
	for _, s := range serverList.Items {
		if s.Spec.Stork != nil && s.Spec.Stork.Enabled {
			requests = append(requests, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: s.Name, Namespace: s.Namespace},
			})
		}
	}
	return requests
}

// listStorkEnabledDhcp6 lists KeaDhcp6Servers with Stork enabled in the given namespace.
func listStorkEnabledDhcp6(ctx context.Context, c client.Reader, namespace string) []ctrl.Request {
	serverList := &keav1alpha1.KeaDhcp6ServerList{}
	if err := c.List(ctx, serverList, client.InNamespace(namespace)); err != nil {
		return nil
	}
	var requests []ctrl.Request
	for _, s := range serverList.Items {
		if s.Spec.Stork != nil && s.Spec.Stork.Enabled {
			requests = append(requests, ctrl.Request{
				NamespacedName: types.NamespacedName{Name: s.Name, Namespace: s.Namespace},
			})
		}
	}
	return requests
}

// fillPeerURLs populates empty peer URLs in the HA config using the StatefulSet
// naming convention. Each peer at index i gets:
//
//	http://<stsName>-<i>.<hlSvcName>.<namespace>.svc.cluster.local:<port>/
//
// This allows users to omit URLs and let the operator derive them automatically.
func fillPeerURLs(ha *keav1alpha1.HAConfig, namespace, crName, component string, port int32) {
	if ha == nil {
		return
	}
	stsName := resources.StatefulSetName(crName, component)
	hlSvcName := resources.HeadlessServiceName(crName, component)
	for i := range ha.Peers {
		if ha.Peers[i].URL == "" {
			ha.Peers[i].URL = fmt.Sprintf("http://%s-%d.%s.%s.svc.cluster.local:%d/",
				stsName, i, hlSvcName, namespace, port)
		}
	}
}

// operatorAnnotations returns a copy of the annotations the operator owns.
func operatorAnnotations(annotations map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range annotations {
		if strings.HasPrefix(k, resources.OperatorAnnotationPrefix) {
			out[k] = v
		}
	}
	return out
}

// mergeOperatorAnnotations sets the operator-owned object annotations and
// leaves every other annotation (for example the Deployment revision kept by
// Kubernetes) untouched.
func mergeOperatorAnnotations(obj metav1.Object, owned map[string]string) {
	if len(owned) == 0 {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	for k, v := range owned {
		annotations[k] = v
	}
	obj.SetAnnotations(annotations)
}
