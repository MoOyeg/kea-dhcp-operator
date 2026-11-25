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
	"fmt"
	"path"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// defaultLeaseClaimSize is the size of each HA peer's lease claim when the
// spec does not set one. Lease files are small.
var defaultLeaseClaimSize = resource.MustParse("1Gi")

// leaseStoragePlan is the validated lease storage of a DHCP server.
type leaseStoragePlan struct {
	storageType keav1alpha1.LeaseStorageType
	// claimName is the user's claim (ExistingClaim).
	claimName string
	// claim describes the per-ordinal claims (ClaimTemplate).
	claim *resources.LeaseClaimParams
}

// durable reports whether the lease file survives the replacement of a pod.
func (p leaseStoragePlan) durable() bool {
	return p.storageType == keav1alpha1.LeaseStorageExistingClaim || p.storageType == keav1alpha1.LeaseStorageClaimTemplate
}

// planLeaseStorage validates the lease storage of a server. Durable storage
// only makes sense for memfile leases written to the mounted directory, and
// each kind of claim only for the workload it can serve safely: a claim the
// user provides holds one lease file, so one standalone writer; HA peers need
// one claim each.
func planLeaseStorage(ls *keav1alpha1.LeaseStorage, db *keav1alpha1.DatabaseConfig, ha bool, replicas int32) (leaseStoragePlan, error) {
	if ls == nil || ls.Type == "" || ls.Type == keav1alpha1.LeaseStorageEphemeral {
		return leaseStoragePlan{storageType: keav1alpha1.LeaseStorageEphemeral}, nil
	}
	if db != nil {
		if db.Type != "" && db.Type != keav1alpha1.DatabaseTypeMemfile {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage %s only applies to memfile leases; lease-database type %s keeps leases outside the pods already", ls.Type, db.Type)
		}
		if db.Persist != nil && !*db.Persist {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage %s needs lease-database persist: true; with persist: false Kea never writes a lease file", ls.Type)
		}
		if db.Name != "" && !strings.HasPrefix(path.Clean(db.Name), resources.LeaseMountPath+"/") {
			return leaseStoragePlan{}, fmt.Errorf("lease-database name %q is outside %s, the directory leaseStorage %s mounts", db.Name, resources.LeaseMountPath, ls.Type)
		}
	}
	switch ls.Type {
	case keav1alpha1.LeaseStorageExistingClaim:
		if ls.ClaimName == "" {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage ExistingClaim needs claimName")
		}
		if ha {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage ExistingClaim is for standalone servers; use ClaimTemplate so each HA peer has its own lease file")
		}
		if replicas > 1 {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage ExistingClaim supports one replica; %d servers would write the same lease file", replicas)
		}
		return leaseStoragePlan{storageType: ls.Type, claimName: ls.ClaimName}, nil
	case keav1alpha1.LeaseStorageClaimTemplate:
		if !ha {
			return leaseStoragePlan{}, fmt.Errorf("leaseStorage ClaimTemplate is for HA servers; use ExistingClaim for a standalone server")
		}
		size := defaultLeaseClaimSize
		if ls.Size != nil {
			if ls.Size.Sign() <= 0 {
				return leaseStoragePlan{}, fmt.Errorf("leaseStorage size must be positive, got %s", ls.Size.String())
			}
			size = *ls.Size
		}
		return leaseStoragePlan{storageType: ls.Type, claim: &resources.LeaseClaimParams{Size: size, StorageClassName: ls.StorageClassName}}, nil
	}
	return leaseStoragePlan{}, fmt.Errorf("unknown leaseStorage type %q", ls.Type)
}

// leasesDurable reports whether a server's leases outlive its pods: a
// database backend, or memfile leases on a persistent volume.
func leasesDurable(db *keav1alpha1.DatabaseConfig, storage leaseStoragePlan) bool {
	return leasesSurvivePodReplacement(db) || storage.durable()
}

// reportLeasesDurable sets the LeasesDurable condition.
func reportLeasesDurable(conditions *[]keav1alpha1.ConditionStatus, db *keav1alpha1.DatabaseConfig, storage leaseStoragePlan) {
	switch {
	case leasesSurvivePodReplacement(db):
		setCondition(conditions, keav1alpha1.ConditionTypeLeasesDurable, "True", "DatabaseBackend", "Leases are kept in the lease database")
	case storage.durable():
		setCondition(conditions, keav1alpha1.ConditionTypeLeasesDurable, "True", "PersistentVolume", "Memfile leases are kept on a persistent volume")
	case db != nil && db.Persist != nil && !*db.Persist:
		setCondition(conditions, keav1alpha1.ConditionTypeLeasesDurable, "False", "InMemory", "Memfile leases are kept in memory only (persist: false) and are lost when Kea restarts")
	default:
		setCondition(conditions, keav1alpha1.ConditionTypeLeasesDurable, "False", "EmptyDir",
			"Memfile leases are kept in an emptyDir and are lost when a pod is replaced; set leaseStorage to keep them on a persistent volume")
	}
}

// claimTemplateSignature describes claim templates by the fields the
// operator sets, ignoring server-side defaults, so an unchanged spec compares
// equal to what the API server stored.
func claimTemplateSignature(vcts []corev1.PersistentVolumeClaim) string {
	parts := make([]string, 0, len(vcts))
	for _, v := range vcts {
		class := ""
		if v.Spec.StorageClassName != nil {
			class = *v.Spec.StorageClassName
		}
		modes := make([]string, 0, len(v.Spec.AccessModes))
		for _, m := range v.Spec.AccessModes {
			modes = append(modes, string(m))
		}
		sort.Strings(modes)
		size := v.Spec.Resources.Requests[corev1.ResourceStorage]
		parts = append(parts, fmt.Sprintf("%s|%s|%s|%s", v.Name, class, size.String(), strings.Join(modes, ",")))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// desiredClaimTemplateSignature is claimTemplateSignature for the claims a
// storage plan produces.
func desiredClaimTemplateSignature(storage leaseStoragePlan) string {
	if storage.claim == nil {
		return ""
	}
	return claimTemplateSignature([]corev1.PersistentVolumeClaim{{
		ObjectMeta: metav1.ObjectMeta{Name: resources.LeaseVolumeName},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: storage.claim.StorageClassName,
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: storage.claim.Size}},
		},
	}})
}
