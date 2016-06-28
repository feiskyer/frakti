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
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/golang/glog"
	"github.com/hyperhq/hyperd/types"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

const (
	hyperRuntimeName    = "hyper"
	minimumHyperVersion = "0.6.0"

	// default resources while the resource limit of kubelet pod is not specified.
	defaultCPUNumber         = 1
	defaultMemoryinMegabytes = 128

	// timeout in second for interacting with hyperd's gRPC API.
	hyperConnectionTimeout = 300 * time.Second
)

// HyperRuntime is the HyperContainer implementation of kubelet runtime API
type HyperRuntime struct {
	client *Client
}

// NewHyperRuntime creates a new Runtime
func NewHyperRuntime(hyperEndpoint string) (*HyperRuntime, error) {
	hyperClient, err := NewClient(hyperEndpoint, hyperConnectionTimeout)
	if err != nil {
		glog.Fatalf("Initialize hyper client failed: %v", err)
		return nil, err
	}

	version, _, err := hyperClient.Version()
	if err != nil {
		glog.Fatalf("Get hyperd version failed: %v", err)
		return nil, err
	}

	glog.V(3).Infof("Got hyperd version: %s", version)
	if check, err := checkVersion(version); !check {
		return nil, err
	}

	return &HyperRuntime{client: hyperClient}, nil
}

// checkVersion checks whether hyperd's version is >=minimumHyperVersion
func checkVersion(version string) (bool, error) {
	hyperVersion, err := semver.NewVersion(version)
	if err != nil {
		glog.Errorf("Make semver failed: %v", version)
		return false, err
	}
	minVersion, err := semver.NewVersion(minimumHyperVersion)
	if err != nil {
		glog.Errorf("Make semver failed: %v", minimumHyperVersion)
		return false, err
	}
	if hyperVersion.LessThan(*minVersion) {
		return false, fmt.Errorf("Hyperd version is older than %s", minimumHyperVersion)
	}

	return true, nil
}

// Version returns the runtime name, runtime version and runtime API version
func (h *HyperRuntime) Version() (string, string, string, error) {
	version, apiVersion, err := h.client.Version()
	if err != nil {
		glog.Errorf("Get hyper version failed: %v", err)
		return "", "", "", err
	}

	return hyperRuntimeName, version, apiVersion, nil
}

// CreatePodSandbox creates a pod-level sandbox.
// The definition of PodSandbox is at https://github.com/kubernetes/kubernetes/pull/25899
func (h *HyperRuntime) CreatePodSandbox(config *kubeapi.PodSandboxConfig) (string, error) {
	// TODO: support pod-level portmappings in upstream hyperd
	spec := &types.UserPod{
		Id:       config.GetName(),
		Hostname: config.GetHostname(),
		Labels:   config.Labels,
	}

	// Pass annotations in labels
	if spec.Labels == nil {
		spec.Labels = make(map[string]string)
	}
	for k, v := range config.Annotations {
		spec.Labels[k] = v
	}

	// Make dns
	if config.DnsOptions != nil {
		// TODO: support DNS search domains in upstream hyperd
		spec.Dns = config.DnsOptions.Servers
	}

	// Make UserResource
	vcpu := defaultCPUNumber
	memory := defaultMemoryinMegabytes
	if config.Resources != nil {
		if config.Resources.Cpu != nil && config.Resources.Cpu.GetLimits() > 0 {
			vcpu = int(config.Resources.Cpu.GetLimits() + 0.5)
			if vcpu == 0 {
				vcpu = 1
			}
		}

		if config.Resources.Memory != nil && config.Resources.Memory.GetLimits() > 0 {
			memory = int(config.Resources.Memory.GetLimits() / 1024 / 1024)
		}
	}
	spec.Resource = &types.UserResource{
		Vcpu:   int32(vcpu),
		Memory: int32(memory),
	}

	podID, err := h.client.CreatePod(spec)
	if err != nil {
		glog.Errorf("Create pod %s failed: %v", config.GetName(), err)
		return "", err
	}

	err = h.client.StartPod(podID)
	if err != nil {
		glog.Errorf("Start pod %s failed: %v", podID, err)
		return "", err
	}

	return podID, nil
}

// StopPodSandbox stops the sandbox. If there are any running containers in the
// sandbox, they should be force terminated.
func (h *HyperRuntime) StopPodSandbox(podSandBoxID string) error {
	code, cause, err := h.client.StopPod(podSandBoxID)
	if err != nil {
		glog.Errorf("Stop pod %s failed, code: %d, cause: %s, error: %v", podSandBoxID, code, cause, err)
		return err
	}

	return nil
}

// DeletePodSandbox deletes the sandbox. If there are any running containers in the
// sandbox, they should be force deleted.
func (h *HyperRuntime) DeletePodSandbox(podSandBoxID string) error {
	err := h.client.RemovePod(podSandBoxID)
	if err != nil {
		glog.Errorf("Remove pod %s failed: %v", podSandBoxID, err)
		return err
	}

	return nil
}

// PodSandboxStatus returns the Status of the PodSandbox.
func (h *HyperRuntime) PodSandboxStatus(podSandBoxID string) (*kubeapi.PodSandboxStatus, error) {
	info, err := h.client.GetPodInfo(podSandBoxID)
	if err != nil {
		glog.Errorf("GetPodInfo for %s failed: %v", podSandBoxID, err)
		return nil, err
	}

	state := toPodSandboxState(info.Status.Phase)
	podIP := ""
	if len(info.Status.PodIP) > 0 {
		podIP = info.Status.PodIP[0]
	}

	podStatus := &kubeapi.PodSandboxStatus{
		Id:        &podSandBoxID,
		Name:      &info.PodName,
		State:     &state,
		Network:   &kubeapi.PodSandboxNetworkStatus{Ip: &podIP},
		CreatedAt: &info.CreatedAt,
		Labels:    info.Spec.Labels,
	}

	return podStatus, nil
}

// ListPodSandbox returns a list of SandBox.
func (h *HyperRuntime) ListPodSandbox(filter *kubeapi.PodSandboxFilter) ([]*kubeapi.PodSandboxListItem, error) {
	pods, err := h.client.GetPodList()
	if err != nil {
		glog.Errorf("GetPodList failed: %v", err)
		return nil, err
	}

	items := make([]*kubeapi.PodSandboxListItem, 0, len(pods))
	for _, pod := range pods {
		state := toPodSandboxState(pod.Status)

		if filter != nil {
			if filter.Name != nil && pod.PodName != filter.GetName() {
				continue
			}

			if filter.Id != nil && pod.PodID != filter.GetId() {
				continue
			}

			if filter.State != nil && state != filter.GetState() {
				continue
			}
		}

		if filter != nil && filter.LabelSelector != nil &&
			!inMap(filter.LabelSelector, pod.Labels) {
			continue
		}

		items = append(items, &kubeapi.PodSandboxListItem{
			Id:        &pod.PodID,
			Name:      &pod.PodName,
			Labels:    pod.Labels,
			State:     &state,
			CreatedAt: &pod.CreatedAt,
		})
	}

	return items, nil
}

// CreateContainer creates a new container in specified PodSandbox
func (h *HyperRuntime) CreateContainer(podSandBoxID string, config *kubeapi.ContainerConfig, sandboxConfig *kubeapi.PodSandboxConfig) (string, error) {
	if config.GetPrivileged() {
		return "", fmt.Errorf("Priviledged containers are not supported in hyper")
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

	containerID, err := h.client.CreateContainer(podSandBoxID, containerSpec)
	if err != nil {
		glog.Errorf("Create container %s in pod %s failed: %v", config.GetName(), podSandBoxID, err)
		return "", err
	}

	return containerID, nil
}

// StartContainer starts the container.
func (h *HyperRuntime) StartContainer(rawContainerID string) error {
	err := h.client.StartContainer(rawContainerID)
	if err != nil {
		glog.Errorf("Start container %s failed: %v", rawContainerID, err)
		return err
	}

	return nil
}

// StopContainer stops a running container with a grace period (i.e., timeout).
func (h *HyperRuntime) StopContainer(rawContainerID string, timeout int64) error {
	err := h.client.StopContainer(rawContainerID, timeout)
	if err != nil {
		glog.Errorf("Stop container %s failed: %v", rawContainerID, err)
		return err
	}

	return nil
}

// RemoveContainer removes the container. If the container is running, the container
// should be force removed.
func (h *HyperRuntime) RemoveContainer(rawContainerID string) error {
	err := h.client.RemoveContainer(rawContainerID)
	if err != nil {
		glog.Errorf("Remove container %s failed: %v", rawContainerID, err)
		return err
	}

	return nil
}

// ListContainers lists all containers by filters.
func (h *HyperRuntime) ListContainers(filter *kubeapi.ContainerFilter) ([]*kubeapi.Container, error) {
	containerList, err := h.client.GetContainerList(false)
	if err != nil {
		glog.Errorf("Get container list failed: %v", err)
		return nil, err
	}

	containers := make([]*kubeapi.Container, 0, len(containerList))
	for _, c := range containerList {
		containerName := strings.TrimPrefix(c.ContainerName, "/")
		if filter != nil {
			if filter.Name != nil && containerName != filter.GetName() {
				continue
			}

			if filter.Id != nil && c.ContainerID != filter.GetId() {
				continue
			}

			if filter.PodSandboxId != nil && c.PodID != filter.GetPodSandboxId() {
				continue
			}
		}

		info, err := h.client.GetContainerInfo(c.ContainerID)
		if err != nil {
			glog.Errorf("Get container info for %s failed: %v", c.ContainerID, err)
			return nil, err
		}

		state := toKubeContainerState(info.Status.Phase)
		if filter != nil {
			if filter.State != nil && state != filter.GetState() {
				continue
			}

			if filter.LabelSelector != nil && !inMap(filter.LabelSelector, info.Container.Labels) {
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

	return containers, nil
}

// ContainerStatus returns the container status.
func (h *HyperRuntime) ContainerStatus(containerID string) (*kubeapi.ContainerStatus, error) {
	status, err := h.client.GetContainerInfo(containerID)
	if err != nil {
		glog.Errorf("Get container info for %s failed: %v", containerID, err)
		return nil, err
	}

	podInfo, err := h.client.GetPodInfo(status.PodID)
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

	return kubeStatus, nil
}

// Exec execute a command in the container.
func (h *HyperRuntime) Exec(rawContainerID string, cmd []string, tty bool, stdin io.Reader, stdout, stderr io.WriteCloser) error {
	// TODO: implement exec in container
	return fmt.Errorf("Not implemented")
}

// ListImages lists existing images.
func (h *HyperRuntime) ListImages(filter *kubeapi.ImageFilter) ([]*kubeapi.Image, error) {
	images, err := h.client.GetImageList()
	if err != nil {
		glog.Errorf("Get image list failed: %v", err)
		return nil, err
	}

	var results []*kubeapi.Image
	for _, img := range images {
		if filter != nil {
			filter := filter.Image.GetImage()
			if !strings.Contains(filter, ":") {
				filter = filter + ":latest"
			}

			if !inList(filter, img.RepoTags) {
				continue
			}
		}

		imageSize := uint64(img.VirtualSize)
		results = append(results, &kubeapi.Image{
			Id:          &img.Id,
			RepoTags:    img.RepoTags,
			RepoDigests: img.RepoDigests,
			Size_:       &imageSize,
		})
	}

	glog.V(4).Infof("Got imageList: %q", results)
	return results, nil
}

// ImageStatus returns the status of the image.
func (h *HyperRuntime) ImageStatus(image *kubeapi.ImageSpec) (*kubeapi.Image, error) {
	// TODO: implement ImageStatus
	return nil, fmt.Errorf("Not implemented")
}

// PullImage pulls a image with authentication config.
func (h *HyperRuntime) PullImage(image *kubeapi.ImageSpec, authConfig *kubeapi.AuthConfig) error {
	img := image.GetImage()
	repo, tag := parseRepositoryTag(img)
	auth := getHyperAuthConfig(authConfig)

	err := h.client.PullImage(repo, tag, auth, nil)
	if err != nil {
		glog.Errorf("Pull image %s failed: %v", image, err)
		return err
	}

	return nil
}

// RemoveImage removes the image.
func (h *HyperRuntime) RemoveImage(image *kubeapi.ImageSpec) error {
	img := image.GetImage()
	err := h.client.RemoveImage(img)
	if err != nil {
		glog.Errorf("Remove image %s failed: %v", img, err)
		return err
	}

	return nil
}
