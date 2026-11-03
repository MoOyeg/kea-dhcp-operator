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

package resources

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// TestBuildStatefulSet_Deterministic guards against the pod template changing
// between reconciles. The init script and the NAD address script used to be
// built by ranging over Go maps. Map order is random, so every reconcile could
// produce a "new" template and roll all HA pods. Restarting one peer then took
// the other one down too.
func TestBuildStatefulSet_Deterministic(t *testing.T) {
	sts := StatefulSetName(testCRName, ComponentDhcp4)
	hl := HeadlessServiceName(testCRName, ComponentDhcp4)
	params := func() StatefulSetParams {
		peers := map[string]string{}
		addrs := map[int]string{}
		for i := 0; i < 4; i++ {
			peers[fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local", sts, i, hl, testNamespace)] =
				fmt.Sprintf("%s-%d.%s.svc.cluster.local", sts, i, testNamespace)
			addrs[i] = fmt.Sprintf("10.200.0.%d", 10+i)
		}
		return StatefulSetParams{
			CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
			Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
			ConfigMapNames:  map[int]string{0: "test-dhcp4-0", 1: "test-dhcp4-1"},
			PeerHostnameMap: peers, PeerAddresses: addrs,
			NADInterface: testNADIface, NADSubnet: testNADSubnet,
		}
	}

	first := BuildStatefulSet(params()).Spec.Template
	for i := 0; i < 50; i++ {
		got := BuildStatefulSet(params()).Spec.Template
		if !reflect.DeepEqual(got.Spec.InitContainers, first.Spec.InitContainers) {
			t.Fatalf("build %d: init container differs between identical builds", i)
		}
		if !reflect.DeepEqual(got.Spec.Containers, first.Spec.Containers) {
			t.Fatalf("build %d: containers differ between identical builds", i)
		}
	}

	// The peers appear in sorted order, so the result is also readable.
	script := first.Spec.InitContainers[0].Command[2]
	prev := -1
	for i := 0; i < 4; i++ {
		idx := strings.Index(script, fmt.Sprintf(`echo "%s-%d.%s`, sts, i, hl))
		if idx < 0 || idx < prev {
			t.Fatalf("peer %d out of order in init script (index %d after %d)", i, idx, prev)
		}
		prev = idx
	}
}
