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
package gcp

import (
	"testing"

	"github.com/golang/mock/gomock"

	googleapi "google.golang.org/api/googleapi"

	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/gcp/client/mock"
)

const (
	infraID   = "test-x595d"
	projectID = "test"
)

func TestPrepareSubmarinerClusterEnv(t *testing.T) {
	cases := []struct {
		name           string
		input          api.PrepareForSubmarinerInput
		expectInvoking func(client *mock.MockInterface, gc *gcpCloud)
	}{
		{
			name: "build submariner env",
			input: api.PrepareForSubmarinerInput{
				PublicPorts: []api.PortSpec{
					{
						Protocol: "udp",
						Port:     500,
					},
					{
						Protocol: "udp",
						Port:     4500,
					},
				},
				InternalPorts: []api.PortSpec{
					{
						Protocol: "udp",
						Port:     4800,
					},
					{
						Protocol: "tcp",
						Port:     8080,
					},
				},
			},
			expectInvoking: func(client *mock.MockInterface, gc *gcpCloud) {
				// get rules
				client.EXPECT().GetFirewallRule(
					"test", "test-x595d-submariner-public-ports-ingress").Return(nil, &googleapi.Error{Code: 404})
				client.EXPECT().GetFirewallRule(
					"test", "test-x595d-submariner-public-ports-egress").Return(nil, &googleapi.Error{Code: 404})
				client.EXPECT().GetFirewallRule(
					"test", "test-x595d-submariner-internal-ports-ingress").Return(nil, &googleapi.Error{Code: 404})

				// instert rules
				ingress, egress := newExternalFirewallRules(projectID, infraID, []api.PortSpec{
					{Protocol: "udp", Port: 500},
					{Protocol: "udp", Port: 4500},
				})
				client.EXPECT().InsertFirewallRule("test", ingress).Return(nil)
				client.EXPECT().InsertFirewallRule("test", egress).Return(nil)

				internal := newInternalFirewallRule(projectID, infraID, []api.PortSpec{
					{Protocol: "udp", Port: 4800},
					{Protocol: "tcp", Port: 8080},
				})
				client.EXPECT().InsertFirewallRule("test", internal).Return(nil)
			},
		},
		{
			name: "rebuild submariner env - update",
			input: api.PrepareForSubmarinerInput{
				PublicPorts: []api.PortSpec{
					{
						Protocol: "udp",
						Port:     501,
					},
					{
						Protocol: "udp",
						Port:     4501,
					},
				},
				InternalPorts: []api.PortSpec{
					{
						Protocol: "udp",
						Port:     4800,
					},
					{
						Protocol: "tcp",
						Port:     8080,
					},
				},
			},
			expectInvoking: func(client *mock.MockInterface, gc *gcpCloud) {
				// get rules
				ingress, egress := newExternalFirewallRules(projectID, infraID, []api.PortSpec{
					{Protocol: "udp", Port: 500},
					{Protocol: "udp", Port: 4500},
				})
				client.EXPECT().GetFirewallRule(projectID, "test-x595d-submariner-public-ports-ingress").Return(ingress, nil)
				client.EXPECT().GetFirewallRule(projectID, "test-x595d-submariner-public-ports-egress").Return(egress, nil)
				internal := newInternalFirewallRule(projectID, infraID, []api.PortSpec{
					{Protocol: "udp", Port: 4800},
					{Protocol: "tcp", Port: 8080},
				})
				client.EXPECT().GetFirewallRule(projectID, "test-x595d-submariner-internal-ports-ingress").Return(internal, nil)

				// udpate rules
				newIngress, newEgress := newExternalFirewallRules(projectID, infraID, []api.PortSpec{
					{Protocol: "udp", Port: 501},
					{Protocol: "udp", Port: 4501},
				})
				client.EXPECT().UpdateFirewallRule(projectID, "test-x595d-submariner-public-ports-ingress", newIngress).Return(nil)
				client.EXPECT().UpdateFirewallRule(projectID, "test-x595d-submariner-public-ports-egress", newEgress).Return(nil)
				client.EXPECT().UpdateFirewallRule(projectID, "test-x595d-submariner-internal-ports-ingress", internal).Return(nil)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockClient := mock.NewMockInterface(mockCtrl)

			gc := NewCloud(projectID, infraID, mockClient)

			c.expectInvoking(mockClient, gc.(*gcpCloud))

			err := gc.PrepareForSubmariner(c.input, &mockReporter{})
			if err != nil {
				t.Errorf("expect no err, bug got %v", err)
			}
		})
	}
}

func TestCleanUpSubmarinerClusterEnv(t *testing.T) {
	cases := []struct {
		name           string
		expectInvoking func(*mock.MockInterface)
	}{
		{
			name: "delete submariner env",
			expectInvoking: func(gcpClient *mock.MockInterface) {
				gcpClient.EXPECT().DeleteFirewallRule(projectID, "test-x595d-submariner-public-ports-ingress").Return(nil)
				gcpClient.EXPECT().DeleteFirewallRule(projectID, "test-x595d-submariner-public-ports-egress").Return(nil)
				gcpClient.EXPECT().DeleteFirewallRule(projectID, "test-x595d-submariner-internal-ports-ingress").Return(nil)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockClient := mock.NewMockInterface(mockCtrl)
			c.expectInvoking(mockClient)

			gc := &gcpCloud{
				projectID: projectID,
				infraID:   infraID,
				client:    mockClient,
			}

			err := gc.CleanupAfterSubmariner(&mockReporter{})
			if err != nil {
				t.Errorf("expect no err, bug got %v", err)
			}
		})
	}
}

type mockReporter struct{}

func (*mockReporter) Started(message string, args ...interface{}) {}

func (*mockReporter) Succeeded(message string, args ...interface{}) {}

func (*mockReporter) Failed(errs ...error) {}
