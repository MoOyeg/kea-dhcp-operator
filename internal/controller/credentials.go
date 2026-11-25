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
	"errors"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/kea"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// reasonCredentialsUnavailable is the condition reason for a referenced
// credential Secret or key that does not exist.
const reasonCredentialsUnavailable = "CredentialsUnavailable"

// errCredentialsUnavailable marks a missing referenced Secret or key, as
// opposed to an API error worth retrying.
var errCredentialsUnavailable = errors.New("referenced credential is unavailable")

// secretRef collects how the env vars use one Secret.
type secretRef struct {
	// required is true when at least one reference is not optional, so the
	// Secret must exist.
	required bool
	// keys are the keys non-optional references need.
	keys []string
}

// secretKeyRefs returns every Secret the SecretKeyRef env vars use. Optional
// references are included: "optional" only means the Secret or key may be
// absent, and an existing optional Secret still has to be tracked because
// its changes only reach Kea when the pods restart.
func secretKeyRefs(envVars []corev1.EnvVar) map[string]*secretRef {
	refs := map[string]*secretRef{}
	for _, ev := range envVars {
		if ev.ValueFrom == nil || ev.ValueFrom.SecretKeyRef == nil {
			continue
		}
		sel := ev.ValueFrom.SecretKeyRef
		ref := refs[sel.Name]
		if ref == nil {
			ref = &secretRef{}
			refs[sel.Name] = ref
		}
		if sel.Optional == nil || !*sel.Optional {
			ref.required = true
			ref.keys = append(ref.keys, sel.Key)
		}
	}
	return refs
}

// credentialsRevision resolves every Secret the env vars reference and
// returns a token that changes whenever one of them is updated, deleted and
// recreated, or replaced. It is built from Secret names, UIDs and
// resourceVersions, never from Secret data, so it can sit in a pod template
// annotation. Any update of a referenced Secret, metadata included, changes
// it; that conservative choice means a rotation is never missed. An optional
// Secret that does not exist contributes an "absent" marker, so its
// disappearance and reappearance change the token as well.
//
// A missing required Secret or key returns an error wrapping
// errCredentialsUnavailable. The token is "" when nothing is referenced.
func credentialsRevision(ctx context.Context, c client.Reader, namespace string, envVars []corev1.EnvVar) (string, error) {
	refs := secretKeyRefs(envVars)
	if len(refs) == 0 {
		return "", nil
	}
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]string, 0, len(names))
	for _, name := range names {
		ref := refs[name]
		secret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return "", fmt.Errorf("reading credential Secret %q: %w", name, err)
			}
			if ref.required {
				return "", fmt.Errorf("%w: Secret %q not found", errCredentialsUnavailable, name)
			}
			entries = append(entries, name+"/absent")
			continue
		}
		for _, key := range ref.keys {
			if _, ok := secret.Data[key]; !ok {
				return "", fmt.Errorf("%w: Secret %q has no key %q", errCredentialsUnavailable, name, key)
			}
		}
		entries = append(entries, fmt.Sprintf("%s/%s/%s", name, secret.UID, secret.ResourceVersion))
	}
	return kea.ComputeHash([]byte(strings.Join(entries, "\n"))), nil
}

// withCredentialsRevision returns pod annotations carrying the credentials
// revision. The input map, which may belong to the CR spec, is never modified.
func withCredentialsRevision(annotations map[string]string, token string) map[string]string {
	if token == "" {
		return annotations
	}
	out := make(map[string]string, len(annotations)+1)
	for k, v := range annotations {
		out[k] = v
	}
	out[resources.CredentialsRevisionAnnotation] = token
	return out
}

// credentialRollout is the outcome of decideCredentialRollout.
type credentialRollout struct {
	// stamp is the pod template annotation value; "" leaves it off.
	stamp string
	// applied is the revision the pods run after this reconcile.
	applied string
	// pending is a newer revision held back; "" when there is none.
	pending string
}

// decideCredentialRollout chooses the credentials revision to put on a pod
// template. Changing it restarts the pods, which re-reads the credential env
// vars but also discards anything kept in pod-local storage, notably memfile
// leases on emptyDir. So:
//
//   - a new workload, or one already on the desired revision, simply gets it;
//   - a workload created by a release without this annotation adopts the
//     desired revision without a restart (an upgrade never rolls pods);
//   - a newer revision rolls the pods when a configuration change restarts
//     them anyway, when autoRoll says nothing is lost (database leases, or a
//     component without leases), or when the user approved that exact
//     revision; otherwise it is held back and reported as pending.
//
// existing is the current Deployment or StatefulSet, or nil.
func decideCredentialRollout(existing client.Object, desired, configHash string, autoRoll bool, approved string) credentialRollout {
	if desired == "" {
		return credentialRollout{}
	}
	if existing == nil {
		return credentialRollout{stamp: desired, applied: desired}
	}
	template := podTemplateAnnotations(existing)
	stamped := template[resources.CredentialsRevisionAnnotation]
	applied := stamped
	if applied == "" {
		applied = existing.GetAnnotations()[resources.AppliedCredentialsRevisionAnnotation]
	}
	configRoll := template[resources.ConfigHashAnnotation] != configHash
	switch {
	case applied == desired:
		return credentialRollout{stamp: stamped, applied: desired}
	case applied == "" && !configRoll:
		return credentialRollout{applied: desired}
	case configRoll || autoRoll || approved == desired:
		return credentialRollout{stamp: desired, applied: desired}
	default:
		return credentialRollout{stamp: stamped, applied: applied, pending: desired}
	}
}

// podTemplateAnnotations returns the pod template annotations of a
// Deployment or StatefulSet (never nil), or nil for any other object.
func podTemplateAnnotations(obj client.Object) map[string]string {
	var annotations map[string]string
	switch w := obj.(type) {
	case *appsv1.Deployment:
		annotations = w.Spec.Template.Annotations
	case *appsv1.StatefulSet:
		annotations = w.Spec.Template.Annotations
	default:
		return nil
	}
	if annotations == nil {
		return map[string]string{}
	}
	return annotations
}

// leasesSurvivePodReplacement reports whether a DHCP server's leases outlive
// its pods. Only a database backend qualifies: memfile leases live in an
// emptyDir, and even an HA partner cannot be relied on to resynchronize them
// during a rolling update that has no readiness gate on synchronization.
func leasesSurvivePodReplacement(db *keav1alpha1.DatabaseConfig) bool {
	return db != nil && (db.Type == keav1alpha1.DatabaseTypeMySQL || db.Type == keav1alpha1.DatabaseTypePostgreSQL)
}

// reportCredentialRollout sets or clears CredentialsRolloutPending.
func reportCredentialRollout(conditions *[]keav1alpha1.ConditionStatus, r credentialRollout) {
	if r.pending == "" {
		for _, c := range *conditions {
			if c.Type == keav1alpha1.ConditionTypeCredentialsRolloutPending {
				setCondition(conditions, keav1alpha1.ConditionTypeCredentialsRolloutPending, "False", "CredentialsApplied", "The pods run the current credentials")
				return
			}
		}
		return
	}
	setCondition(conditions, keav1alpha1.ConditionTypeCredentialsRolloutPending, "True", "RestartWouldDiscardLeases", fmt.Sprintf(
		"A referenced credential Secret changed, but restarting the pods would discard the memfile leases they keep in pod-local storage, "+
			"so they still use the previous credentials. The new credentials are applied with the next configuration change, "+
			"or now by setting the annotation %s=%s on this resource (this restarts the pods and loses their leases).",
		resources.ApproveCredentialRolloutAnnotation, r.pending))
}

// planCredentialRollout looks up the current workload (into obj, an empty
// Deployment or StatefulSet) and decides the credentials revision for its pod
// template; see decideCredentialRollout.
func planCredentialRollout(ctx context.Context, c client.Reader, key types.NamespacedName, obj client.Object,
	desired, configHash string, autoRoll bool, approved string) (credentialRollout, error) {
	if err := c.Get(ctx, key, obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return credentialRollout{}, fmt.Errorf("reading workload %s: %w", key.Name, err)
		}
		return decideCredentialRollout(nil, desired, configHash, autoRoll, approved), nil
	}
	return decideCredentialRollout(obj, desired, configHash, autoRoll, approved), nil
}

// withAppliedCredentials records the applied credentials revision on a
// workload object's own metadata.
func withAppliedCredentials(obj client.Object, applied string) {
	if applied == "" {
		return
	}
	mergeOperatorAnnotations(obj, map[string]string{resources.AppliedCredentialsRevisionAnnotation: applied})
}

// markCredentialsUnavailable records a missing credential. The caller writes
// the status and stops before touching the workload, so a running server
// keeps its current pods; the Secret watch reconciles again once the Secret
// appears.
func markCredentialsUnavailable(conditions *[]keav1alpha1.ConditionStatus, phase *string, err error) {
	setCondition(conditions, keav1alpha1.ConditionTypeDegraded, "True", reasonCredentialsUnavailable, err.Error())
	setCondition(conditions, keav1alpha1.ConditionTypeReady, "False", reasonCredentialsUnavailable, "A referenced credential is unavailable")
	*phase = keav1alpha1.PhaseError
}

// credentialsGate resolves the credentials revision of a component. When a
// referenced credential is missing it records that in the status, emits a
// Warning event and returns done=true with a nil error, so the reconcile stops
// before the workload is touched and does not spin; the Secret watch brings it
// back once the Secret exists. Other errors return done=true with the error.
func credentialsGate(ctx context.Context, c client.Client, rec record.EventRecorder, obj client.Object,
	conditions *[]keav1alpha1.ConditionStatus, phase *string, envVars []corev1.EnvVar) (token string, done bool, err error) {
	token, err = credentialsRevision(ctx, c, obj.GetNamespace(), envVars)
	if err == nil {
		return token, false, nil
	}
	if !errors.Is(err, errCredentialsUnavailable) {
		return "", true, err
	}
	markCredentialsUnavailable(conditions, phase, err)
	if statusErr := c.Status().Update(ctx, obj); statusErr != nil {
		return "", true, fmt.Errorf("updating status: %w", statusErr)
	}
	rec.Event(obj, corev1.EventTypeWarning, reasonCredentialsUnavailable, err.Error())
	return "", true, nil
}

// dbSecretNames returns the credential Secret names of the given databases.
func dbSecretNames(dbs ...*keav1alpha1.DatabaseConfig) []string {
	var names []string
	for _, db := range dbs {
		if db != nil && db.CredentialsSecretRef != nil {
			names = append(names, db.CredentialsSecretRef.Name)
		}
	}
	return names
}

func dhcp4SecretNames(s *keav1alpha1.KeaDhcp4Server) []string {
	dbs := make([]*keav1alpha1.DatabaseConfig, 0, 2+len(s.Spec.HostsDatabases))
	dbs = append(dbs, s.Spec.LeaseDatabase, s.Spec.HostsDatabase)
	for i := range s.Spec.HostsDatabases {
		dbs = append(dbs, &s.Spec.HostsDatabases[i])
	}
	return dbSecretNames(dbs...)
}

func dhcp6SecretNames(s *keav1alpha1.KeaDhcp6Server) []string {
	dbs := make([]*keav1alpha1.DatabaseConfig, 0, 2+len(s.Spec.HostsDatabases))
	dbs = append(dbs, s.Spec.LeaseDatabase, s.Spec.HostsDatabase)
	for i := range s.Spec.HostsDatabases {
		dbs = append(dbs, &s.Spec.HostsDatabases[i])
	}
	return dbSecretNames(dbs...)
}

func ctrlAgentSecretNames(a *keav1alpha1.KeaControlAgent) []string {
	if a.Spec.Authentication == nil {
		return nil
	}
	var names []string
	for _, c := range a.Spec.Authentication.Clients {
		if c.PasswordSecretKeyRef != nil {
			names = append(names, c.PasswordSecretKeyRef.Name)
		}
	}
	return names
}

func ddnsSecretNames(d *keav1alpha1.KeaDhcpDdns) []string {
	var names []string
	for _, k := range d.Spec.TSIGKeys {
		if k.SecretRef != nil {
			names = append(names, k.SecretRef.Name)
		}
	}
	return names
}

// enqueueReferencingCRs returns a handler.MapFunc that enqueues the CRs in a
// Secret's namespace whose credentials reference that Secret. list returns
// each CR's request together with the Secret names it references.
func enqueueReferencingCRs(c client.Reader, list func(ctx context.Context, c client.Reader, namespace string) map[types.NamespacedName][]string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		var requests []ctrl.Request
		for key, names := range list(ctx, c, obj.GetNamespace()) {
			for _, n := range names {
				if n == obj.GetName() {
					requests = append(requests, ctrl.Request{NamespacedName: key})
					break
				}
			}
		}
		return requests
	}
}

func listDhcp4SecretRefs(ctx context.Context, c client.Reader, namespace string) map[types.NamespacedName][]string {
	list := &keav1alpha1.KeaDhcp4ServerList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	out := make(map[types.NamespacedName][]string, len(list.Items))
	for i := range list.Items {
		out[client.ObjectKeyFromObject(&list.Items[i])] = dhcp4SecretNames(&list.Items[i])
	}
	return out
}

func listDhcp6SecretRefs(ctx context.Context, c client.Reader, namespace string) map[types.NamespacedName][]string {
	list := &keav1alpha1.KeaDhcp6ServerList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	out := make(map[types.NamespacedName][]string, len(list.Items))
	for i := range list.Items {
		out[client.ObjectKeyFromObject(&list.Items[i])] = dhcp6SecretNames(&list.Items[i])
	}
	return out
}

func listCtrlAgentSecretRefs(ctx context.Context, c client.Reader, namespace string) map[types.NamespacedName][]string {
	list := &keav1alpha1.KeaControlAgentList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	out := make(map[types.NamespacedName][]string, len(list.Items))
	for i := range list.Items {
		out[client.ObjectKeyFromObject(&list.Items[i])] = ctrlAgentSecretNames(&list.Items[i])
	}
	return out
}

func listDdnsSecretRefs(ctx context.Context, c client.Reader, namespace string) map[types.NamespacedName][]string {
	list := &keav1alpha1.KeaDhcpDdnsList{}
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil
	}
	out := make(map[types.NamespacedName][]string, len(list.Items))
	for i := range list.Items {
		out[client.ObjectKeyFromObject(&list.Items[i])] = ddnsSecretNames(&list.Items[i])
	}
	return out
}

// anyOf combines map functions, dropping duplicate requests.
func anyOf(fns ...handler.MapFunc) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []ctrl.Request {
		seen := map[types.NamespacedName]bool{}
		var out []ctrl.Request
		for _, fn := range fns {
			for _, req := range fn(ctx, obj) {
				if !seen[req.NamespacedName] {
					seen[req.NamespacedName] = true
					out = append(out, req)
				}
			}
		}
		return out
	}
}
