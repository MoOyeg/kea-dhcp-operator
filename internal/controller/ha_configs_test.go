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
	"errors"
	"strings"
	"testing"

	"github.com/openshift/ocp-kea-dhcp/internal/kea"
)

// fakeHARenderer renders "cfg-<name>" and fails for the names in failFor.
type fakeHARenderer struct {
	failFor  map[string]bool
	rendered []string
}

func (f *fakeHARenderer) RenderJSONWithServerName(name string) ([]byte, error) {
	f.rendered = append(f.rendered, name)
	if f.failFor[name] {
		return nil, errors.New("render failed for " + name)
	}
	return []byte("cfg-" + name), nil
}

// The plan hash must keep the construction used before planHAConfigs
// existed; otherwise upgrading the operator would roll every HA pod.
func TestPlanHAConfigsHashIsUnchangedFromPreviousReleases(t *testing.T) {
	r := &fakeHARenderer{}
	plan, err := planHAConfigs(r, haDhcp4Server(2).Spec.HighAvailability, 2)
	if err != nil {
		t.Fatal(err)
	}
	legacy := kea.ComputeHash([]byte(kea.ComputeHash([]byte("cfg-"+testPeerPrimary)) + kea.ComputeHash([]byte("cfg-"+testPeerSecond))))
	if plan.hash != legacy {
		t.Errorf("plan hash %q, want the previous construction %q", plan.hash, legacy)
	}
	if len(plan.configs) != 2 || string(plan.configs[1]) != "cfg-"+testPeerSecond {
		t.Errorf("configs = %q, want one per ordinal in peer order", plan.configs)
	}
}

// A failure on a later ordinal returns no plan at all, so nothing for the
// earlier ordinals can be written.
func TestPlanHAConfigsLaterOrdinalFailureReturnsNoPlan(t *testing.T) {
	r := &fakeHARenderer{failFor: map[string]bool{testPeerSecond: true}}
	plan, err := planHAConfigs(r, haDhcp4Server(2).Spec.HighAvailability, 2)
	if plan != nil || err == nil || !strings.Contains(err.Error(), `ordinal 1 (peer "server2")`) {
		t.Fatalf("plan=%v err=%v, want no plan and an error naming ordinal 1", plan, err)
	}
	if len(r.rendered) != 2 {
		t.Errorf("rendered %v, want both ordinals attempted", r.rendered)
	}
}

func TestPlanHAConfigsFewerReplicasThanPeers(t *testing.T) {
	plan, err := planHAConfigs(&fakeHARenderer{}, haDhcp4Server(1).Spec.HighAvailability, 1)
	if err != nil || len(plan.configs) != 1 {
		t.Fatalf("plan=%v err=%v, want one ordinal (an external second peer is allowed)", plan, err)
	}
}
