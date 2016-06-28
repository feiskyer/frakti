/*
Copyright 2016 The Kubernetes Authors All rights reserved.
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

package manager

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/hyperhq/hyperd/types"
	"golang.org/x/net/context"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

// CreateContainer creates a new container in specified PodSandbox
func (s *KubeHyperManager) CreateContainer(ctx context.Context, req *kubeapi.CreateContainerRequest) (*kubeapi.CreateContainerResponse, error) {
	glog.V(3).Infof("CreateContainer with request %s", req.String())

	config := req.GetConfig()
	if config.GetPrivileged() {
		return nil, fmt.Errorf("Priviledged containers are not supported in hyper")
	}

	containerSpec := &types.UserContainer{
		Name:       config.GetName(),
		Image:      config.Image.GetImage(),
		Workdir:    config.GetWorkingDir(),
		Tty:        config.GetTty(),
		Command:    config.GetArgs(),
		Entrypoint: config.GetCommand(),
	}

	// TODO: support container-level port-mapping in upstream hyperd

	// TODO: support adding volumes with hostpath for new containers in upstream hyperd
	// volumes := make([]*types.UserVolumeReference, len(config.Mounts))
	// for idx, v := range config.Mounts {
	//	volumes[idx] = &types.UserVolumeReference{
	//		Volume:   v.GetName(),
	//		Path:     v.GetContainerPath(),
	//		ReadOnly: v.GetReadonly(),
	//	}
	//}
	//containerSpec.Volumes = volumes

	// make environments
	environments := make([]*types.EnvironmentVar, len(config.Envs))
	for idx, env := range config.Envs {
		environments[idx] = &types.EnvironmentVar{
			Env:   env.GetKey(),
			Value: env.GetValue(),
		}
	}
	containerSpec.Envs = environments

	// Pass in annotations by labels
	containerSpec.Labels = config.GetLabels()
	if containerSpec.Labels == nil {
		containerSpec.Labels = make(map[string]string)
	}
	for k, v := range config.Annotations {
		containerSpec.Labels[k] = v
	}

	podID := req.GetPodSandboxId()
	containerID, err := s.client.CreateContainer(podID, containerSpec)
	if err != nil {
		glog.Errorf("Create container %s in pod %s failed: %v", req.Config.GetName(), podID, err)
		return nil, err
	}

	return &kubeapi.CreateContainerResponse{ContainerId: &containerID}, nil
}

// StartContainer starts the container.
func (s *KubeHyperManager) StartContainer(ctx context.Context, req *kubeapi.StartContainerRequest) (*kubeapi.StartContainerResponse, error) {
	glog.V(3).Infof("StartContainer with request %s", req.String())

	containerID := req.GetContainerId()
	err := s.client.StartContainer(containerID)
	if err != nil {
		glog.Errorf("Start container %s failed: %v", containerID, err)
		return nil, err
	}

	return &kubeapi.StartContainerResponse{}, nil
}

// StopContainer stops a running container with a grace period (i.e., timeout).
func (s *KubeHyperManager) StopContainer(ctx context.Context, req *kubeapi.StopContainerRequest) (*kubeapi.StopContainerResponse, error) {
	glog.V(3).Infof("StopContainer with request %s", req.String())

	containerID := req.GetContainerId()
	err := s.client.StopContainer(containerID, req.GetTimeout())
	if err != nil {
		glog.Errorf("Stop container %s failed: %v", containerID, err)
		return nil, err
	}

	return &kubeapi.StopContainerResponse{}, nil
}

// RemoveContainer removes the container.
func (s *KubeHyperManager) RemoveContainer(ctx context.Context, req *kubeapi.RemoveContainerRequest) (*kubeapi.RemoveContainerResponse, error) {
	glog.V(3).Infof("RemoveContainer with request %s", req.String())

	containerID := req.GetContainerId()
	err := s.client.RemoveContainer(containerID)
	if err != nil {
		glog.Errorf("Remove container %s failed: %v", containerID, err)
		return nil, err
	}

	return &kubeapi.RemoveContainerResponse{}, nil
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

// ListContainers lists all containers by filters.
func (s *KubeHyperManager) ListContainers(ctx context.Context, req *kubeapi.ListContainersRequest) (*kubeapi.ListContainersResponse, error) {
	glog.V(3).Infof("ListContainers with request %s", req.String())

	containerList, err := s.client.GetContainerList(false)
	if err != nil {
		glog.Errorf("Get container list failed: %v", err)
		return nil, err
	}

	containers := make([]*kubeapi.Container, 0, len(containerList))
	for _, c := range containerList {
		containerName := strings.TrimPrefix(c.ContainerName, "/")
		if req.Filter != nil {
			if req.Filter.Name != nil && containerName != req.Filter.GetName() {
				continue
			}

			if req.Filter.Id != nil && c.ContainerID != req.Filter.GetId() {
				continue
			}

			if req.Filter.PodSandboxId != nil && c.PodID != req.Filter.GetPodSandboxId() {
				continue
			}
		}

		info, err := s.client.GetContainerInfo(c.ContainerID)
		if err != nil {
			glog.Errorf("Get container info for %s failed: %v", c.ContainerID, err)
			return nil, err
		}

		state := toKubeContainerState(info.Status.Phase)
		if req.Filter != nil {
			if req.Filter.State != nil && state != req.Filter.GetState() {
				continue
			}

			if req.Filter.LabelSelector != nil && !inMap(req.Filter.LabelSelector, info.Container.Labels) {
				continue
			}
		}

		containers = append(containers, &kubeapi.Container{
			Id:       &c.ContainerID,
			Name:     &containerName,
			Image:    &kubeapi.ImageSpec{Image: &info.Container.Image},
			ImageRef: &info.Container.ImageID,
			Labels:   info.Container.Labels,
			State:    &state,
		})
	}

	return &kubeapi.ListContainersResponse{
		Containers: containers,
	}, nil
}

// ContainerStatus returns the container status.
func (s *KubeHyperManager) ContainerStatus(ctx context.Context, req *kubeapi.ContainerStatusRequest) (*kubeapi.ContainerStatusResponse, error) {
	glog.V(3).Infof("ContainerStatus with request %s", req.String())

	containerID := req.GetContainerId()
	status, err := s.client.GetContainerInfo(containerID)
	if err != nil {
		glog.Errorf("Get container info for %s failed: %v", containerID, err)
		return nil, err
	}

	podInfo, err := s.client.GetPodInfo(status.PodID)
	if err != nil {
		glog.Errorf("Get pod info for %s failed: %v", status.PodID, err)
		return nil, err
	}

	state := toKubeContainerState(status.Status.Phase)
	containerName := strings.TrimPrefix(status.Container.Name, "/")
	kubeStatus := &kubeapi.ContainerStatus{
		Id:        &status.Container.ContainerID,
		Image:     &kubeapi.ImageSpec{Image: &status.Container.Image},
		ImageRef:  &status.Container.ImageID,
		Name:      &containerName,
		State:     &state,
		Labels:    status.Container.Labels,
		CreatedAt: &status.CreatedAt,
	}

	mounts := make([]*kubeapi.Mount, len(status.Container.VolumeMounts))
	for idx, mnt := range status.Container.VolumeMounts {
		mounts[idx] = &kubeapi.Mount{
			Name:          &mnt.Name,
			ContainerPath: &mnt.MountPath,
			Readonly:      &mnt.ReadOnly,
		}

		for _, v := range podInfo.Spec.Volumes {
			if v.Name == mnt.Name {
				mounts[idx].HostPath = &v.Source
			}
		}
	}
	kubeStatus.Mounts = mounts

	switch status.Status.Phase {
	case "running":
		startedAt, err := parseTimeString(status.Status.Running.StartedAt)
		if err != nil {
			glog.Errorf("Hyper: can't parse startedAt %s", status.Status.Running.StartedAt)
			return nil, err
		}
		kubeStatus.StartedAt = &startedAt
	case "failed", "succeeded":
		startedAt, err := parseTimeString(status.Status.Terminated.StartedAt)
		if err != nil {
			glog.Errorf("Hyper: can't parse startedAt %s", status.Status.Terminated.StartedAt)
			return nil, err
		}
		finishedAt, err := parseTimeString(status.Status.Terminated.FinishedAt)
		if err != nil {
			glog.Errorf("Hyper: can't parse finishedAt %s", status.Status.Terminated.FinishedAt)
			return nil, err
		}

		kubeStatus.StartedAt = &startedAt
		kubeStatus.FinishedAt = &finishedAt
		kubeStatus.Reason = &status.Status.Terminated.Reason
		kubeStatus.ExitCode = &status.Status.Terminated.ExitCode
	default:
		kubeStatus.Reason = &status.Status.Waiting.Reason
	}

	return &kubeapi.ContainerStatusResponse{
		Status: kubeStatus,
	}, nil
}

// Exec execute a command in the container.
func (s *KubeHyperManager) Exec(stream kubeapi.RuntimeService_ExecServer) error {
	return fmt.Errorf("Not implemented")
}
