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

package resources

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const testLeaseClaim = "kea-leases-claim"

func leaseVolume(volumes []corev1.Volume) *corev1.Volume {
	for i := range volumes {
		if volumes[i].Name == LeaseVolumeName {
			return &volumes[i]
		}
	}
	return nil
}

// A standalone server with a claim mounts it for the lease file and uses the
// Recreate strategy: a rolling update would run two pods writing the same
// lease file (and an RWO volume cannot attach to two nodes anyway).
func TestDeploymentLeaseClaim(t *testing.T) {
	params := DeploymentParams{CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapName: testDeployConfigMap}

	plain := BuildDeployment(params)
	if v := leaseVolume(plain.Spec.Template.Spec.Volumes); v == nil || v.EmptyDir == nil {
		t.Errorf("default lease volume is not an emptyDir: %+v", v)
	}
	if plain.Spec.Strategy.Type == appsv1.RecreateDeploymentStrategyType {
		t.Error("default Deployment uses Recreate")
	}

	params.LeaseClaimName = testLeaseClaim
	durable := BuildDeployment(params)
	v := leaseVolume(durable.Spec.Template.Spec.Volumes)
	if v == nil || v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != testLeaseClaim {
		t.Fatalf("lease volume is not the claim: %+v", v)
	}
	if durable.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("strategy %q, want Recreate", durable.Spec.Strategy.Type)
	}
	mounted := false
	for _, m := range durable.Spec.Template.Spec.Containers[0].VolumeMounts {
		mounted = mounted || (m.Name == LeaseVolumeName && m.MountPath == LeaseMountPath)
	}
	if !mounted {
		t.Error("lease volume not mounted at the lease path")
	}
}

// An HA server with a claim template gets one claim per ordinal, no emptyDir
// for leases, and claims that survive deletion and scale-down.
func TestStatefulSetLeaseClaimTemplate(t *testing.T) {
	class := "fast"
	two := int32(2)
	params := StatefulSetParams{CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		Replicas: &two, ConfigMapNames: map[int]string{0: testCM0, 1: testCM1}}
	if plain := BuildStatefulSet(params); len(plain.Spec.VolumeClaimTemplates) != 0 {
		t.Error("default StatefulSet has claim templates")
	}

	params.LeaseClaim = &LeaseClaimParams{Size: resource.MustParse("2Gi"), StorageClassName: &class}
	sts := BuildStatefulSet(params)
	if v := leaseVolume(sts.Spec.Template.Spec.Volumes); v != nil {
		t.Errorf("lease volume still defined in the pod template: %+v", v)
	}
	if len(sts.Spec.VolumeClaimTemplates) != 1 {
		t.Fatalf("claim templates %+v, want one", sts.Spec.VolumeClaimTemplates)
	}
	vct := sts.Spec.VolumeClaimTemplates[0]
	if vct.Name != LeaseVolumeName || *vct.Spec.StorageClassName != class ||
		vct.Spec.Resources.Requests.Storage().String() != "2Gi" ||
		len(vct.Spec.AccessModes) != 1 || vct.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("claim template %+v", vct)
	}
	policy := sts.Spec.PersistentVolumeClaimRetentionPolicy
	if policy == nil || policy.WhenDeleted != appsv1.RetainPersistentVolumeClaimRetentionPolicyType ||
		policy.WhenScaled != appsv1.RetainPersistentVolumeClaimRetentionPolicyType {
		t.Errorf("retention policy %+v, want Retain/Retain", policy)
	}
}
