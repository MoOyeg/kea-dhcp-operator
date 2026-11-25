/*
Copyright 2024.

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
	"regexp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// withConfigRevision returns a copy of labels marking a staged object with
// its configuration revision.
func withConfigRevision(labels map[string]string, rev string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out[resources.ConfigRevisionLabel] = rev
	return out
}

// recordConfigRevision records on the StatefulSet object which revision its
// template references and, when that changes, which one it replaced; the
// previous revision's objects are kept so the StatefulSet can be rolled back.
func recordConfigRevision(sts *appsv1.StatefulSet, existing client.Object, rev string) {
	previous := ""
	if existing != nil {
		anns := existing.GetAnnotations()
		previous = anns[resources.PreviousConfigRevisionAnnotation]
		if current := anns[resources.ConfigRevisionAnnotation]; current != "" && current != rev {
			previous = current
		}
	}
	owned := map[string]string{resources.ConfigRevisionAnnotation: rev}
	if previous != "" {
		owned[resources.PreviousConfigRevisionAnnotation] = previous
	}
	mergeOperatorAnnotations(sts, owned)
}

// reader returns the uncached reader for pods.
func (r *KeaDhcp4ServerReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// collectStaleHAConfigs deletes the per-ordinal ConfigMaps and configuration
// Secrets of this server that nothing can still use. Kept are: the objects
// the StatefulSet template mounts, every object of its current and previous
// revision, and every object a live pod of the server mounts (pods are read
// without the cache, which only holds Stork agent pods). Only objects this CR
// controls are considered. Failures are logged and retried on the next
// reconcile; they never fail the reconcile or touch a referenced object.
func (r *KeaDhcp4ServerReconciler) collectStaleHAConfigs(ctx context.Context, server *keav1alpha1.KeaDhcp4Server,
	component string, sts *appsv1.StatefulSet) {
	logger := log.FromContext(ctx)
	keepNames := map[string]bool{}
	addVolumes := func(volumes []corev1.Volume) {
		for _, v := range volumes {
			if v.ConfigMap != nil {
				keepNames["cm/"+v.ConfigMap.Name] = true
			}
			if v.Secret != nil {
				keepNames["secret/"+v.Secret.SecretName] = true
			}
		}
	}
	addVolumes(sts.Spec.Template.Spec.Volumes)
	keepRevs := map[string]bool{}
	for _, key := range []string{resources.ConfigRevisionAnnotation, resources.PreviousConfigRevisionAnnotation} {
		if rev := sts.Annotations[key]; rev != "" {
			keepRevs[rev] = true
		}
	}

	selector := client.MatchingLabels(resources.CommonLabels(server.Name, component))
	pods := &corev1.PodList{}
	if err := r.reader().List(ctx, pods, client.InNamespace(server.Namespace), selector); err != nil {
		logger.Error(err, "listing pods before removing stale configuration revisions; will retry")
		return
	}
	for i := range pods.Items {
		addVolumes(pods.Items[i].Spec.Volumes)
	}

	stale := staleConfigObjects(server, component)
	cms := &corev1.ConfigMapList{}
	secrets := &corev1.SecretList{}
	if err := r.List(ctx, cms, client.InNamespace(server.Namespace), selector); err != nil {
		logger.Error(err, "listing ConfigMaps for configuration cleanup; will retry")
		return
	}
	if err := r.List(ctx, secrets, client.InNamespace(server.Namespace), selector); err != nil {
		logger.Error(err, "listing Secrets for configuration cleanup; will retry")
		return
	}
	var candidates []client.Object
	for i := range cms.Items {
		if stale(&cms.Items[i], "cm/", keepNames, keepRevs) {
			candidates = append(candidates, &cms.Items[i])
		}
	}
	for i := range secrets.Items {
		if stale(&secrets.Items[i], "secret/", keepNames, keepRevs) {
			candidates = append(candidates, &secrets.Items[i])
		}
	}
	for _, obj := range candidates {
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "removing a stale configuration revision; will retry", "name", obj.GetName())
			continue
		}
		logger.Info("removed a configuration revision no longer in use", "name", obj.GetName())
	}
}

// staleConfigObjects returns a predicate for per-ordinal configuration
// objects of this server that nothing keeps: staged objects (with a revision
// label) and the in-place objects of releases before staging
// (<cr>-<component>-<ordinal> and <cr>-<component>-config-<ordinal>).
// The standalone ConfigMap and Secret never match.
func staleConfigObjects(server *keav1alpha1.KeaDhcp4Server, component string) func(client.Object, string, map[string]bool, map[string]bool) bool {
	legacy := regexp.MustCompile(fmt.Sprintf(`^%s-%s-(config-)?[0-9]+$`, regexp.QuoteMeta(server.Name), regexp.QuoteMeta(component)))
	return func(obj client.Object, kind string, keepNames, keepRevs map[string]bool) bool {
		if !metav1.IsControlledBy(obj, server) || keepNames[kind+obj.GetName()] {
			return false
		}
		if rev, staged := obj.GetLabels()[resources.ConfigRevisionLabel]; staged {
			return !keepRevs[rev]
		}
		return legacy.MatchString(obj.GetName())
	}
}
