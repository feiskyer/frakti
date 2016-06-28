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

package hyper

import (
	"io"
	"time"

	"github.com/hyperhq/hyperd/types"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// Client is the gRPC client for hyperd
type Client struct {
	client  types.PublicAPIClient
	timeout time.Duration
}

// NewClient creates a new hyper client
func NewClient(server string, timeout time.Duration) (*Client, error) {
	conn, err := grpc.Dial(server, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return &Client{
		client:  types.NewPublicAPIClient(conn),
		timeout: timeout,
	}, nil
}

func (c *Client) getContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.timeout)
}

// GetPodInfo gets pod info by podID
func (c *Client) GetPodInfo(podID string) (*types.PodInfo, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	request := types.PodInfoRequest{
		PodID: podID,
	}
	pod, err := c.client.PodInfo(ctx, &request)
	if err != nil {
		return nil, err
	}

	return pod.PodInfo, nil
}

// GetPodList get a list of Pods
func (c *Client) GetPodList() ([]*types.PodListResult, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	request := types.PodListRequest{}
	podList, err := c.client.PodList(
		ctx,
		&request,
	)
	if err != nil {
		return nil, err
	}

	return podList.PodList, nil
}

// GetContainerList gets a list of containers
func (c *Client) GetContainerList(auxiliary bool) ([]*types.ContainerListResult, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := types.ContainerListRequest{
		Auxiliary: auxiliary,
	}
	containerList, err := c.client.ContainerList(
		ctx,
		&req,
	)
	if err != nil {
		return nil, err
	}

	return containerList.ContainerList, nil
}

// GetContainerInfo gets container info by container name or id
func (c *Client) GetContainerInfo(container string) (*types.ContainerInfo, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := types.ContainerInfoRequest{
		Container: container,
	}
	cinfo, err := c.client.ContainerInfo(
		ctx,
		&req,
	)
	if err != nil {
		return nil, err
	}

	return cinfo.ContainerInfo, nil
}

// GetImageList gets a list of images
func (c *Client) GetImageList() ([]*types.ImageInfo, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := types.ImageListRequest{}
	imageList, err := c.client.ImageList(
		ctx,
		&req,
	)
	if err != nil {
		return nil, err
	}

	return imageList.ImageList, nil
}

// CreatePod creates a pod
func (c *Client) CreatePod(spec *types.UserPod) (string, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := types.PodCreateRequest{
		PodSpec: spec,
	}
	resp, err := c.client.PodCreate(
		ctx,
		&req,
	)
	if err != nil {
		return "", err
	}

	return resp.PodID, nil
}

// StartContainer starts a hyper container
func (c *Client) StartContainer(containerID string) error {
	// Hyperd doesn't support start container yet, so here is a workaround
	// to start container by restarting its pod.
	// TODO: Implement StartContainer in hyperd's native start container API
	info, err := c.GetContainerInfo(containerID)
	if err != nil {
		return err
	}

	_, _, err = c.StopPod(info.PodID)
	if err != nil {
		return err
	}

	err = c.StartPod(info.PodID)
	if err != nil {
		return err
	}

	return nil
}

// StopContainer stops a hyper container
func (c *Client) StopContainer(containerID string, timeout int64) error {
	// This is a workaround for not interrupting container lifecycle management.
	// It should be replaced by real stop action while upstream hyperd supported.
	// The container would be stopped automatically while stopping its pod.
	// TODO: Implement StopContainer
	return nil
}

// RemoveContainer stops a hyper container
func (c *Client) RemoveContainer(containerID string) error {
	// This is a workaround for not interrupting  container lifecycle management.
	// It should be replaced by real delete action while upstream hyperd supported.
	// The container would be deleted automatically while deleting its pod.
	// TODO: Implement RemoveContainer
	return nil
}

// CreateContainer creates a container
func (c *Client) CreateContainer(podID string, spec *types.UserContainer) (string, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	req := types.ContainerCreateRequest{
		PodID:         podID,
		ContainerSpec: spec,
	}

	resp, err := c.client.ContainerCreate(ctx, &req)
	if err != nil {
		return "", err
	}

	return resp.ContainerID, nil
}

// RemovePod removes a pod by podID
func (c *Client) RemovePod(podID string) error {
	ctx, cancel := c.getContext()
	defer cancel()

	_, err := c.client.PodRemove(
		ctx,
		&types.PodRemoveRequest{PodID: podID},
	)

	if err != nil {
		return err
	}

	return nil
}

// StartPod starts a pod by podID
func (c *Client) StartPod(podID string) error {
	ctx, cancel := c.getContext()
	defer cancel()

	stream, err := c.client.PodStart(ctx)
	if err != nil {
		return err
	}

	req := types.PodStartMessage{
		PodID: podID,
	}
	if err := stream.Send(&req); err != nil {
		return err
	}

	if _, err := stream.Recv(); err != nil {
		return err
	}

	return nil
}

// StopPod stops a pod
func (c *Client) StopPod(podID string) (int, string, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	resp, err := c.client.PodStop(ctx, &types.PodStopRequest{
		PodID: podID,
	})
	if err != nil {
		return -1, "", err
	}

	return int(resp.Code), resp.Cause, nil
}

// PullImage pulls a image from registry
func (c *Client) PullImage(image, tag string, auth *types.AuthConfig, out io.Writer) error {
	ctx, cancel := c.getContext()
	defer cancel()

	request := types.ImagePullRequest{
		Image: image,
		Tag:   tag,
		Auth:  auth,
	}
	stream, err := c.client.ImagePull(ctx, &request)
	if err != nil {
		return err
	}

	for {
		res, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if out != nil {
			n, err := out.Write(res.Data)
			if err != nil {
				return err
			}
			if n != len(res.Data) {
				return io.ErrShortWrite
			}
		}
	}

	return nil
}

// RemoveImage removes a image from hyperd
func (c *Client) RemoveImage(image string) error {
	ctx, cancel := c.getContext()
	defer cancel()

	_, err := c.client.ImageRemove(ctx, &types.ImageRemoveRequest{Image: image})
	return err
}

// Version gets hyperd version
func (c *Client) Version() (string, string, error) {
	ctx, cancel := c.getContext()
	defer cancel()

	resp, err := c.client.Version(ctx, &types.VersionRequest{})
	if err != nil {
		return "", "", err
	}

	return resp.Version, resp.ApiVersion, nil
}
