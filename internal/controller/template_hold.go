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
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// templateHold is the outcome of decideTemplateHold.
type templateHold struct {
	// keep is the existing pod template to reapply unchanged; nil to apply
	// the desired one.
	keep *corev1.PodTemplateSpec
	// revision and generation are recorded on the workload object.
	revision   string
	generation string
	// pending is the newer TemplateRevision held back; "" when none.
	pending string
}

// getWorkload returns the Deployment (or, for HA, StatefulSet) at key, or
// nil when it does not exist.
func getWorkload(ctx context.Context, c client.Reader, key types.NamespacedName, statefulSet bool) (client.Object, error) {
	var obj client.Object = &appsv1.Deployment{}
	if statefulSet {
		obj = &appsv1.StatefulSet{}
	}
	if err := c.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading workload %s: %w", key.Name, err)
	}
	return obj, nil
}

// podTemplate returns the pod template of a Deployment or StatefulSet.
func podTemplate(obj client.Object) *corev1.PodTemplateSpec {
	switch w := obj.(type) {
	case *appsv1.Deployment:
		return &w.Spec.Template
	case *appsv1.StatefulSet:
		return &w.Spec.Template
	}
	return nil
}

// decideTemplateHold keeps the existing pod template of a workload rendered
// by an earlier TemplateRevision when restarting its pods would discard
// memfile leases kept in pod-local storage. An operator upgrade alone must
// not restart such pods: the replacement of two HA peers a few seconds apart
// leaves both with empty lease files.
//
// The new template is applied as soon as the pods restart for a reason of
// their own anyway: a configuration change, a credentials rollout already
// decided by decideCredentialRollout, or any spec change (a new CR
// generation). It is also applied when the user approves the restart, or
// when leases survive restarts (database backend). Changes within the same
// TemplateRevision, such as a Stork agent token, are never held.
func decideTemplateHold(existing client.Object, owner metav1.Object, durable bool, configHash, credStamp string) templateHold {
	gen := strconv.FormatInt(owner.GetGeneration(), 10)
	apply := templateHold{revision: resources.TemplateRevision, generation: gen}
	if existing == nil || durable {
		return apply
	}
	annotations := existing.GetAnnotations()
	applied := annotations[resources.TemplateRevisionAnnotation]
	if applied == "" {
		applied = resources.LegacyTemplateRevision
	}
	if applied == resources.TemplateRevision {
		return apply
	}
	tmpl := podTemplate(existing)
	current := tmpl.Annotations
	configRoll := current[resources.ConfigHashAnnotation] != configHash
	credRoll := current[resources.CredentialsRevisionAnnotation] != credStamp
	appliedGen := annotations[resources.AppliedGenerationAnnotation]
	specChanged := appliedGen != "" && appliedGen != gen
	approved := owner.GetAnnotations()[resources.ApproveRestartAnnotation] == resources.TemplateRevision
	if configRoll || credRoll || specChanged || approved {
		return apply
	}
	return templateHold{keep: tmpl.DeepCopy(), revision: applied, generation: gen, pending: resources.TemplateRevision}
}

// applyTemplateHold puts a held template back on the desired workload and
// records the revision and generation on the workload object.
func applyTemplateHold(desired client.Object, h templateHold) {
	if h.keep != nil {
		*podTemplate(desired) = *h.keep
	}
	mergeOperatorAnnotations(desired, map[string]string{
		resources.TemplateRevisionAnnotation:  h.revision,
		resources.AppliedGenerationAnnotation: h.generation,
	})
}

// reportRestartPending sets or clears RestartPending.
func reportRestartPending(conditions *[]keav1alpha1.ConditionStatus, h templateHold) {
	if h.pending == "" {
		for _, c := range *conditions {
			if c.Type == keav1alpha1.ConditionTypeRestartPending {
				setCondition(conditions, keav1alpha1.ConditionTypeRestartPending, "False", "TemplateCurrent", "The pods run the current operator's pod template")
				return
			}
		}
		return
	}
	setCondition(conditions, keav1alpha1.ConditionTypeRestartPending, "True", "RestartWouldDiscardLeases", fmt.Sprintf(
		"This operator release renders the pods differently (template revision %s), but restarting them would discard the memfile leases "+
			"they keep in pod-local storage, so they keep their current template. It is applied with the next spec or configuration change, "+
			"or now by setting the annotation %s=%s on this resource (this restarts the pods and loses their leases unless you preserve them "+
			"as described in docs/ha.md).",
		h.pending, resources.ApproveRestartAnnotation, h.pending))
}
