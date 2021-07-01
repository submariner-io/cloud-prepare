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
package client

import (
	"context"
	"net/http"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

//go:generate mockgen -source=./client.go -destination=./mock/client_generated.go -package=mock

// Interface wraps an actual GCP library client to allow for easier testing.
type Interface interface {
	InsertFirewallRule(projectID string, rule *compute.Firewall) error
	GetFirewallRule(projectID, name string) (*compute.Firewall, error)
	DeleteFirewallRule(projectID, name string) error
	UpdateFirewallRule(projectID, name string, rule *compute.Firewall) error
}

type gcpClient struct {
	computeClient *compute.Service
}

func (g *gcpClient) InsertFirewallRule(projectID string, rule *compute.Firewall) error {
	_, err := g.computeClient.Firewalls.Insert(projectID, rule).Context(context.TODO()).Do()
	return err
}

func (g *gcpClient) GetFirewallRule(projectID, name string) (*compute.Firewall, error) {
	return g.computeClient.Firewalls.Get(projectID, name).Context(context.TODO()).Do()
}

func (g *gcpClient) DeleteFirewallRule(projectID, name string) error {
	_, err := g.computeClient.Firewalls.Delete(projectID, name).Context(context.TODO()).Do()
	return err
}

func (g *gcpClient) UpdateFirewallRule(projectID, name string, rule *compute.Firewall) error {
	_, err := g.computeClient.Firewalls.Update(projectID, name, rule).Context(context.TODO()).Do()
	return err
}

func NewClient(options []option.ClientOption) (Interface, error) {
	ctx := context.TODO()

	computeClient, err := compute.NewService(ctx, options...)
	if err != nil {
		return nil, err
	}

	return &gcpClient{
		computeClient: computeClient,
	}, nil
}

func IsGCPNotFoundError(err error) bool {
	gerr, ok := err.(*googleapi.Error)
	if !ok {
		return false
	}

	return gerr.Code == http.StatusNotFound
}
