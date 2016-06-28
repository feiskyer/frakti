/*
Copyright 2016 The Kubernetes Authors.

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

package hyper

import (
	"strings"
	"time"

	"github.com/hyperhq/hyperd/types"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

func getHyperAuthConfig(auth *kubeapi.AuthConfig) *types.AuthConfig {
	if auth == nil {
		return &types.AuthConfig{}
	}

	config := &types.AuthConfig{}
	if auth.Username != nil {
		config.Username = auth.GetUsername()
	}
	if auth.Password != nil {
		config.Password = auth.GetPassword()
	}
	if auth.Auth != nil {
		config.Auth = auth.GetAuth()
	}
	if auth.RegistryToken != nil {
		config.Registrytoken = auth.GetRegistryToken()
	}
	if auth.ServerAddress != nil {
		config.Serveraddress = auth.GetServerAddress()
	}

	return config
}

// Get a repos name and returns the right reposName + tag|digest
// The tag can be confusing because of a port in a repository name.
//     Ex: localhost.localdomain:5000/samalba/hipache:latest
//     Digest ex: localhost:5000/foo/bar@sha256:bc8813ea7b3603864987522f02a76101c17ad122e1c46d790efc0fca78ca7bfb
func parseRepositoryTag(repos string) (string, string) {
	n := strings.Index(repos, "@")
	if n >= 0 {
		parts := strings.Split(repos, "@")
		return parts[0], parts[1]
	}
	n = strings.LastIndex(repos, ":")
	if n < 0 {
		return repos, "latest"
	}
	if tag := repos[n+1:]; !strings.Contains(tag, "/") {
		return repos[:n], tag
	}
	return repos, "latest"
}

// inList checks if a string is in a list
func inList(in string, list []string) bool {
	for _, str := range list {
		if in == str {
			return true
		}
	}

	return false
}

// inMap checks if a map is in dest map
func inMap(in, dest map[string]string) bool {
	for k, v := range in {
		if value, ok := dest[k]; ok {
			if value != v {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

func parseTimeString(str string) (int64, error) {
	t := time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)
	if str == "" {
		return t.Unix(), nil
	}

	layout := "2006-01-02T15:04:05Z"
	t, err := time.Parse(layout, str)
	if err != nil {
		return t.Unix(), err
	}

	return t.Unix(), nil
}

func toPodSandboxState(state string) kubeapi.PodSandBoxState {
	if state == "running" || state == "Running" {
		return kubeapi.PodSandBoxState_READY
	}

	return kubeapi.PodSandBoxState_NOTREADY
}

func toKubeContainerState(state string) kubeapi.ContainerState {
	switch state {
	case "running":
		return kubeapi.ContainerState_RUNNING
	case "pending":
		return kubeapi.ContainerState_CREATED
	case "failed", "succeeded":
		return kubeapi.ContainerState_EXITED
	default:
		return kubeapi.ContainerState_UNKNOWN
	}
}
