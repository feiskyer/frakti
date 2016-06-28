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
	"net"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/golang/glog"
	"github.com/hyperhq/hyperd/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"k8s.io/frakti/pkg/hyper"
	kubeapi "k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"
)

const (
	hyperRuntimeName    = "hyper"
	minimumHyperVersion = "0.6.0"
	runtimeAPIVersion   = "0.1.0"

	// default resources while the resource limit of kubelet pod is not specified.
	defaultCPUNumber         = 1
	defaultMemoryinMegabytes = 128

	// timeout in second for interacting with hyperd's gRPC API.
	hyperConnectionTimeout = 300 * time.Second
)

// KubeHyperManager serves the kubelet runtime gRPC api which will be
// consumed by kubelet
type KubeHyperManager struct {
	// The grpc server.
	server *grpc.Server
	// The grpc client of hyperd.
	client *hyper.Client
}

// NewKubeHyperManager creates a new KubeHyperManager
func NewKubeHyperManager(hyperEndpoint string) (*KubeHyperManager, error) {
	hyperClient, err := hyper.NewClient(hyperEndpoint, hyperConnectionTimeout)
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

	s := &KubeHyperManager{
		client: hyperClient,
		server: grpc.NewServer(),
	}
	s.registerServer()

	return s, nil
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

// Serve starts gRPC server at tcp://addr
func (s *KubeHyperManager) Serve(addr string) error {
	glog.V(1).Infof("Start frakti at %s", addr)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		glog.Fatalf("Failed to listen %s: %v", addr, err)
		return err
	}

	return s.server.Serve(lis)
}

func (s *KubeHyperManager) registerServer() {
	kubeapi.RegisterRuntimeServiceServer(s.server, s)
	kubeapi.RegisterImageServiceServer(s.server, s)
}

// Version returns the runtime name, runtime version and runtime API version
func (s *KubeHyperManager) Version(ctx context.Context, req *kubeapi.VersionRequest) (*kubeapi.VersionResponse, error) {
	version, apiVersion, err := s.client.Version()
	if err != nil {
		glog.Errorf("Get hyper version failed: %v", err)
		return nil, err
	}

	runtimeName := hyperRuntimeName
	kubeletAPIVersion := runtimeAPIVersion
	return &kubeapi.VersionResponse{
		Version:           &kubeletAPIVersion,
		RuntimeName:       &runtimeName,
		RuntimeVersion:    &version,
		RuntimeApiVersion: &apiVersion,
	}, nil
}

// CreatePodSandbox creates a hyper Pod
func (s *KubeHyperManager) CreatePodSandbox(ctx context.Context, req *kubeapi.CreatePodSandboxRequest) (*kubeapi.CreatePodSandboxResponse, error) {
	glog.V(3).Infof("CreatePodSandbox with request %s", req.String())

	config := req.Config
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

	podID, err := s.client.CreatePod(spec)
	if err != nil {
		glog.Errorf("Create pod %s failed: %v", config.GetName(), err)
		return nil, err
	}

	err = s.client.StartPod(podID)
	if err != nil {
		glog.Errorf("Start pod %s failed: %v", podID, err)
		return nil, err
	}

	return &kubeapi.CreatePodSandboxResponse{PodSandboxId: &podID}, nil
}

// StopPodSandbox stops the sandbox.
func (s *KubeHyperManager) StopPodSandbox(ctx context.Context, req *kubeapi.StopPodSandboxRequest) (*kubeapi.StopPodSandboxResponse, error) {
	glog.V(3).Infof("StopPodSandbox with request %s", req.String())

	code, cause, err := s.client.StopPod(req.GetPodSandboxId())
	if err != nil {
		glog.Errorf("Remove pod %s failed, code: %d, cause: %s, error: %v", req.GetPodSandboxId(), code, cause, err)
		return nil, err
	}

	return &kubeapi.StopPodSandboxResponse{}, nil
}

// DeletePodSandbox deletes the sandbox.
func (s *KubeHyperManager) DeletePodSandbox(ctx context.Context, req *kubeapi.DeletePodSandboxRequest) (*kubeapi.DeletePodSandboxResponse, error) {
	glog.V(3).Infof("DeletePodSandbox with request %s", req.String())

	err := s.client.RemovePod(req.GetPodSandboxId())
	if err != nil {
		glog.Errorf("Remove pod %s failed: %v", req.GetPodSandboxId(), err)
		return nil, err
	}

	return &kubeapi.DeletePodSandboxResponse{}, nil
}

// PodSandboxStatus returns the Status of the PodSandbox.
func (s *KubeHyperManager) PodSandboxStatus(ctx context.Context, req *kubeapi.PodSandboxStatusRequest) (*kubeapi.PodSandboxStatusResponse, error) {
	glog.V(3).Infof("PodSandboxStatus with request %s", req.String())

	info, err := s.client.GetPodInfo(req.GetPodSandboxId())
	if err != nil {
		glog.Errorf("GetPodInfo for %s failed: %v", req.GetPodSandboxId(), err)
		return nil, err
	}

	state := toPodSandboxState(info.Status.Phase)
	podIP := ""
	if len(info.Status.PodIP) > 0 {
		podIP = info.Status.PodIP[0]
	}

	podStatus := &kubeapi.PodSandboxStatus{
		Id:        req.PodSandboxId,
		Name:      &info.PodName,
		State:     &state,
		Network:   &kubeapi.PodSandboxNetworkStatus{Ip: &podIP},
		CreatedAt: &info.CreatedAt,
		Labels:    info.Spec.Labels,
	}

	return &kubeapi.PodSandboxStatusResponse{Status: podStatus}, nil
}

// ListPodSandbox returns a list of SandBox.
func (s *KubeHyperManager) ListPodSandbox(ctx context.Context, req *kubeapi.ListPodSandboxRequest) (*kubeapi.ListPodSandboxResponse, error) {
	glog.V(3).Infof("ListPodSandbox with request %s", req.String())

	pods, err := s.client.GetPodList()
	if err != nil {
		glog.Errorf("GetPodList failed: %v", err)
		return nil, err
	}

	items := make([]*kubeapi.PodSandboxListItem, 0, len(pods))
	for _, pod := range pods {
		state := toPodSandboxState(pod.Status)

		if req.Filter != nil {
			if req.Filter.Name != nil && pod.PodName != req.Filter.GetName() {
				continue
			}

			if req.Filter.Id != nil && pod.PodID != req.Filter.GetId() {
				continue
			}

			if req.Filter.State != nil && state != req.Filter.GetState() {
				continue
			}
		}

		if req.Filter != nil && req.Filter.LabelSelector != nil &&
			!inMap(req.Filter.LabelSelector, pod.Labels) {
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

	return &kubeapi.ListPodSandboxResponse{Items: items}, nil
}

func toPodSandboxState(state string) kubeapi.PodSandBoxState {
	if state == "running" || state == "Running" {
		return kubeapi.PodSandBoxState_READY
	}

	return kubeapi.PodSandBoxState_NOTREADY
}
