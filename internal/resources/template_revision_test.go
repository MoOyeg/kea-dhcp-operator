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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const templateRevisionGolden = "testdata/template_revision.golden.json"

// templateGolden records the pod templates TemplateRevision stands for.
type templateGolden struct {
	Revision string            `json:"revision"`
	Hashes   map[string]string `json:"hashes"`
}

// templateFixtures renders the pod templates the builders produce for the
// supported workload shapes.
func templateFixtures() map[string]corev1.PodTemplateSpec {
	stork := &StorkSidecarParams{Image: testStorkAgentImage, ServerURL: testStorkServerURL, ServerToken: testStorkServerToken, Port: 8080, PrometheusPort: 9547}
	deploy := func(nad, configSecret bool, s *StorkSidecarParams) corev1.PodTemplateSpec {
		p := DeploymentParams{CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
			Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
			ConfigMapName: testDeployConfigMap, ConfigHash: "hash", StorkAgent: s}
		if configSecret {
			p.ConfigSecretName = testConfigSecret
		}
		if nad {
			p.NADInterface, p.NADSubnet = testNADIface, testNADSubnet
		}
		return BuildDeployment(p).Spec.Template
	}
	sts := func(nad, configSecret bool, s *StorkSidecarParams) corev1.PodTemplateSpec {
		two := int32(2)
		p := StatefulSetParams{CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
			Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
			Replicas: &two, ConfigHash: "hash", ConfigMapNames: map[int]string{0: testCM0, 1: testCM1},
			PeerHostnameMap: map[string]string{"test-dhcp4-0.test-dhcp4-hl": "test-dhcp4-0.default.svc", "test-dhcp4-1.test-dhcp4-hl": "test-dhcp4-1.default.svc"},
			StorkAgent:      s}
		if configSecret {
			p.ConfigSecretNames = map[int]string{0: "test-dhcp4-config-0", 1: "test-dhcp4-config-1"}
		}
		if nad {
			p.NADInterface, p.NADSubnet = testNADIface, testNADSubnet
			p.PeerAddresses = map[int]string{0: "10.200.0.10", 1: "10.200.0.11"}
		}
		return BuildStatefulSet(p).Spec.Template
	}
	return map[string]corev1.PodTemplateSpec{
		"deployment":                 deploy(false, false, nil),
		"deployment-nad":             deploy(true, false, nil),
		"deployment-credentials":     deploy(false, true, nil),
		"deployment-nad-credentials": deploy(true, true, nil),
		"deployment-stork":           deploy(false, false, stork),
		"statefulset":                sts(false, false, nil),
		"statefulset-nad":            sts(true, false, nil),
		"statefulset-credentials":    sts(false, true, nil),
		"statefulset-stork":          sts(false, false, stork),
	}
}

// TestTemplateRevisionMatchesBuilderOutput fails whenever the builders render
// a different pod template without a TemplateRevision bump. Workloads keeping
// memfile leases are only moved to a new template when the revision changes
// (and the user approves or edits the spec), so a silent change would either
// restart them on upgrade or never reach them.
//
// After bumping TemplateRevision, regenerate the golden file:
//
//	UPDATE_GOLDEN=1 go test ./internal/resources/ -run TestTemplateRevisionMatchesBuilderOutput
func TestTemplateRevisionMatchesBuilderOutput(t *testing.T) {
	got := templateGolden{Revision: TemplateRevision, Hashes: map[string]string{}}
	for name, tmpl := range templateFixtures() {
		data, err := json.Marshal(tmpl)
		if err != nil {
			t.Fatal(err)
		}
		got.Hashes[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(templateRevisionGolden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(templateRevisionGolden, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	raw, err := os.ReadFile(templateRevisionGolden)
	if err != nil {
		t.Fatalf("reading %s: %v (generate it with UPDATE_GOLDEN=1)", templateRevisionGolden, err)
	}
	var want templateGolden
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got, want) {
		return
	}
	var changed []string
	for name, h := range got.Hashes {
		if want.Hashes[name] != h {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	if want.Revision == TemplateRevision {
		t.Fatalf("the builders now render different pod templates for %v but TemplateRevision is still %q.\n"+
			"Bump TemplateRevision in internal/resources/template_revision.go, add a history line, and regenerate:\n"+
			"  UPDATE_GOLDEN=1 go test ./internal/resources/ -run TestTemplateRevisionMatchesBuilderOutput", changed, TemplateRevision)
	}
	t.Fatalf("TemplateRevision is %q but %s records %q; regenerate it:\n"+
		"  UPDATE_GOLDEN=1 go test ./internal/resources/ -run TestTemplateRevisionMatchesBuilderOutput",
		TemplateRevision, templateRevisionGolden, want.Revision)
}
