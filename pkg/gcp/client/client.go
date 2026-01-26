/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

//nolint:wrapcheck // The functions are wrappers so let the caller wrap errors.
package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

//go:generate mockery

// Interface wraps an actual GCP library client to allow for easier testing.
type Interface interface {
	InsertFirewallRule(ctx context.Context, projectID string, rule *compute.Firewall) error
	GetFirewallRule(ctx context.Context, projectID, name string) (*compute.Firewall, error)
	DeleteFirewallRule(ctx context.Context, projectID, name string) error
	UpdateFirewallRule(ctx context.Context, projectID, name string, rule *compute.Firewall) error
	GetInstance(ctx context.Context, zone string, instance string) (*compute.Instance, error)
	ListInstances(ctx context.Context, zone string) (*compute.InstanceList, error)
	ListZones(ctx context.Context) (*compute.ZoneList, error)
	InstanceHasPublicIP(ctx context.Context, instance *compute.Instance) (bool, error)
	UpdateInstanceNetworkTags(ctx context.Context, project, zone, instance string, tags *compute.Tags) error
	ConfigurePublicIPOnInstance(ctx context.Context, instance *compute.Instance) error
	DeletePublicIPOnInstance(ctx context.Context, instance *compute.Instance) error
}

type gcpClient struct {
	projectID     string
	computeClient *compute.Service
}

func (g *gcpClient) InsertFirewallRule(ctx context.Context, projectID string, rule *compute.Firewall) error {
	_, err := g.computeClient.Firewalls.Insert(projectID, rule).Context(ctx).Do()
	return err
}

func (g *gcpClient) GetFirewallRule(ctx context.Context, projectID, name string) (*compute.Firewall, error) {
	return g.computeClient.Firewalls.Get(projectID, name).Context(ctx).Do()
}

func (g *gcpClient) DeleteFirewallRule(ctx context.Context, projectID, name string) error {
	_, err := g.computeClient.Firewalls.Delete(projectID, name).Context(ctx).Do()
	return err
}

func (g *gcpClient) UpdateFirewallRule(ctx context.Context, projectID, name string, rule *compute.Firewall) error {
	_, err := g.computeClient.Firewalls.Update(projectID, name, rule).Context(ctx).Do()
	return err
}

func NewClient(ctx context.Context, projectID string, options []option.ClientOption) (Interface, error) {
	computeClient, err := compute.NewService(ctx, options...)
	if err != nil {
		return nil, err
	}

	return &gcpClient{
		projectID:     projectID,
		computeClient: computeClient,
	}, nil
}

func IsGCPNotFoundError(err error) bool {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return gerr.Code == http.StatusNotFound
	}

	return false
}

func (g *gcpClient) GetInstance(ctx context.Context, zone, instance string) (*compute.Instance, error) {
	return g.computeClient.Instances.Get(g.projectID, zone, instance).Context(ctx).Do()
}

func (g *gcpClient) ListInstances(ctx context.Context, zone string) (*compute.InstanceList, error) {
	return g.computeClient.Instances.List(g.projectID, zone).Context(ctx).Do()
}

func (g *gcpClient) ListZones(ctx context.Context) (*compute.ZoneList, error) {
	return g.computeClient.Zones.List(g.projectID).Context(ctx).Do()
}

func (g *gcpClient) InstanceHasPublicIP(ctx context.Context, instance *compute.Instance) (bool, error) {
	networkInterface, err := getNetworkInterface(instance)
	if err != nil {
		return false, err
	}

	return len(networkInterface.AccessConfigs) > 0, nil
}

func (g *gcpClient) UpdateInstanceNetworkTags(ctx context.Context, project, zone, instance string, tags *compute.Tags) error {
	_, err := g.computeClient.Instances.SetTags(project, zone, instance, tags).Context(ctx).Do()

	return err
}

func (g *gcpClient) ConfigurePublicIPOnInstance(ctx context.Context, instance *compute.Instance) error {
	networkInterface, err := getNetworkInterface(instance)
	if err != nil {
		return err
	}

	// The zone of an instance is on URL, so we just need the latest value
	zone := instance.Zone[strings.LastIndex(instance.Zone, "/")+1:]

	// Public IP has already been enabled for this instance
	if len(networkInterface.AccessConfigs) > 0 {
		return nil
	}

	_, err = g.computeClient.Instances.AddAccessConfig(g.projectID, zone, instance.Name,
		networkInterface.Name, &compute.AccessConfig{}).
		Context(ctx).Do()

	return err
}

func (g *gcpClient) DeletePublicIPOnInstance(ctx context.Context, instance *compute.Instance) error {
	networkInterface, err := getNetworkInterface(instance)
	if err != nil {
		return err
	}

	// The zone of an instance is on URL, so we just need the latest value
	zone := instance.Zone[strings.LastIndex(instance.Zone, "/")+1:]
	_, err = g.computeClient.Instances.DeleteAccessConfig(
		g.projectID, zone, instance.Name, "External NAT", networkInterface.Name).
		Context(ctx).Do()

	return err
}

func getNetworkInterface(instance *compute.Instance) (*compute.NetworkInterface, error) {
	if len(instance.NetworkInterfaces) == 0 {
		return nil, fmt.Errorf("there are no network interfaces for instance %s", instance.Name)
	}

	return instance.NetworkInterfaces[0], nil
}
