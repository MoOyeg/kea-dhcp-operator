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

package kea

import (
	"os"
	"os/exec"
	"testing"
)

// keaContainerCheck prepares a test that runs the shipped Kea container
// images. Those tests need podman and network access, so they only run when
// KEA_CONFIG_CHECK=1; once requested, a missing podman is a failure rather
// than a silent skip. KEA_IMAGE_REPO (default docker.cloudsmith.io/isc/docker)
// and KEA_IMAGE_TAG (default 3.0.2) pick the images.
func keaContainerCheck(t *testing.T) (podman, repo, tag string) {
	t.Helper()
	if os.Getenv("KEA_CONFIG_CHECK") != "1" {
		t.Skip("set KEA_CONFIG_CHECK=1 to run the Kea container images")
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatal("KEA_CONFIG_CHECK=1 but podman is not installed")
	}
	repo, tag = os.Getenv("KEA_IMAGE_REPO"), os.Getenv("KEA_IMAGE_TAG")
	if repo == "" {
		repo = "docker.cloudsmith.io/isc/docker"
	}
	if tag == "" {
		tag = "3.0.2"
	}
	return podman, repo, tag
}
