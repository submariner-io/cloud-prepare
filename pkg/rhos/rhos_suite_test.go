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

package rhos_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/pagination"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/rhos"
)

const (
	testInfraID           = "test-infra"
	testRegion            = "test-region"
	testProjectID         = "test-project"
	testInstanceType      = "m1.medium"
	testImage             = "rhcos-test-image"
	testCloudName         = "openstack"
	pagerNameKey          = "pagerNameKey"
	secGroupPager         = "secGroupPager"
	serverID1             = "server1"
	serverID2             = "server2"
	serverID3             = "server3"
	nodeName1             = "node1"
	nodeName2             = "node2"
	nodeName3             = "node3"
	gwSecurityGroup       = testInfraID + rhos.GwSecurityGroupSuffix
	internalSecurityGroup = testInfraID + rhos.InternalSecurityGroupSuffix
)

var ctx = context.TODO()

func TestRHOS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RHOS Suite")
}

type testDriver struct {
	securityGroupsCreated  []string
	rulesCreated           []rules.SecGroupRule
	existingSecurityGroups []secgroups.SecurityGroup
	servers                map[string]*servers.Server
}

func newTestDriver() *testDriver {
	t := &testDriver{}

	BeforeEach(func() {
		t.securityGroupsCreated = nil
		t.rulesCreated = nil
		t.existingSecurityGroups = nil

		t.servers = map[string]*servers.Server{
			nodeName1: {
				ID: serverID1,
			},
			nodeName2: {
				ID: serverID2,
				SecurityGroups: []map[string]any{
					{"name": gwSecurityGroup},
				},
			},
		}

		rhos.NewComputeV2 = func(_ *gophercloud.ProviderClient, _ gophercloud.EndpointOpts) (*gophercloud.ServiceClient, error) {
			return &gophercloud.ServiceClient{}, nil
		}

		rhos.NewNetworkV2 = func(_ *gophercloud.ProviderClient, _ gophercloud.EndpointOpts) (*gophercloud.ServiceClient, error) {
			return &gophercloud.ServiceClient{}, nil
		}

		rhos.ListSecurityGroups = func(c *gophercloud.ServiceClient) pagination.Pager {
			return pagination.Pager{
				Headers: map[string]string{pagerNameKey: secGroupPager},
			}
		}

		rhos.ExtractSecurityGroups = func(page pagination.Page) ([]secgroups.SecurityGroup, error) {
			sgPage, ok := page.(*SecGroupPage)
			Expect(ok).To(BeTrue())

			return sgPage.secGroups, nil
		}

		rhos.CreateSecurityGroup = func(_ context.Context, _ *gophercloud.ServiceClient, o secgroups.CreateOptsBuilder) (
			*secgroups.SecurityGroup, error,
		) {
			name := o.(secgroups.CreateOpts).Name
			t.securityGroupsCreated = append(t.securityGroupsCreated, name)

			return &secgroups.SecurityGroup{Name: name, ID: name}, nil
		}

		rhos.DeleteSecurityGroup = func(_ context.Context, _ *gophercloud.ServiceClient, id string) secgroups.DeleteResult {
			t.existingSecurityGroups = slices.DeleteFunc(t.existingSecurityGroups, func(g secgroups.SecurityGroup) bool {
				return g.ID == id
			})

			return secgroups.DeleteResult{}
		}

		rhos.ListServers = func(_ *gophercloud.ServiceClient, o servers.ListOptsBuilder) pagination.Pager {
			return pagination.Pager{
				Headers: map[string]string{pagerNameKey: o.(servers.ListOpts).Name},
			}
		}

		rhos.ExtractServers = func(page pagination.Page) ([]servers.Server, error) {
			serverPage, ok := page.(*ServerPage)
			Expect(ok).To(BeTrue())

			return serverPage.servers, nil
		}

		rhos.AddServer = func(_ context.Context, _ *gophercloud.ServiceClient, serverID, groupName string) error {
			server := t.findServer(serverID)
			if server != nil && !slices.ContainsFunc(server.SecurityGroups, func(m map[string]any) bool {
				return m["name"] == groupName
			}) {
				server.SecurityGroups = append(server.SecurityGroups, map[string]any{"name": groupName})
			}

			return nil
		}

		rhos.RemoveServer = func(_ context.Context, _ *gophercloud.ServiceClient, serverID, groupName string) error {
			secGroupFn := func(m map[string]any) bool {
				return m["name"] == groupName
			}

			server := t.findServer(serverID)
			if server != nil && slices.ContainsFunc(server.SecurityGroups, secGroupFn) {
				server.SecurityGroups = slices.DeleteFunc(server.SecurityGroups, secGroupFn)
				return nil
			}

			return gophercloud.ErrUnexpectedResponseCode{Actual: http.StatusNotFound}
		}

		rhos.EachPage = func(ctx context.Context, pager pagination.Pager, handler func(context.Context, pagination.Page) (bool, error)) error {
			var page pagination.Page
			if pager.Headers[pagerNameKey] == secGroupPager {
				page = &SecGroupPage{secGroups: t.existingSecurityGroups}
			} else if pager.Headers[pagerNameKey] == testInfraID {
				// Return all servers when listing by infraID
				allServers := make([]servers.Server, 0, len(t.servers))
				for _, server := range t.servers {
					allServers = append(allServers, *server)
				}

				page = &ServerPage{servers: allServers}
			} else {
				server, ok := t.servers[pager.Headers[pagerNameKey]]
				if ok {
					page = &ServerPage{servers: []servers.Server{*server}}
				}
			}

			Expect(page).NotTo(BeNil(), "No Pager for "+pager.Headers[pagerNameKey])

			_, err := handler(ctx, page)
			if err != nil {
				return err
			}

			return nil
		}

		rhos.CreateRule = func(_ context.Context, _ *gophercloud.ServiceClient, o rules.CreateOptsBuilder) (
			*rules.SecGroupRule, error,
		) {
			opts := o.(rules.CreateOpts)

			rule := &rules.SecGroupRule{
				PortRangeMin:   opts.PortRangeMin,
				PortRangeMax:   opts.PortRangeMax,
				Protocol:       string(opts.Protocol),
				RemoteGroupID:  opts.RemoteGroupID,
				RemoteIPPrefix: opts.RemoteIPPrefix,
				SecGroupID:     opts.SecGroupID,
			}
			t.rulesCreated = append(t.rulesCreated, *rule)

			return rule, nil
		}
	})

	return t
}

func (t *testDriver) findServer(serverID string) *servers.Server {
	for _, server := range t.servers {
		if server.ID == serverID {
			return server
		}
	}

	return nil
}

func (t *testDriver) assertRuleCreated(group string, port api.PortSpec) {
	Expect(slices.ContainsFunc(t.rulesCreated, func(r rules.SecGroupRule) bool {
		//nolint:gosec // Ignore integer overflow conversion
		return strings.Contains(r.SecGroupID, group) && uint16(r.PortRangeMin) == port.Port && r.Protocol == port.Protocol
	})).To(BeTrue(), "No rule found for group %q, port %#v. Actual %s", group, port, resource.ToJSON(t.rulesCreated))
}

func (t *testDriver) assertServerSecGroup(groupName string) {
	secGroupFn := func(m map[string]any) bool {
		return m["name"] == groupName
	}

	for _, s := range t.servers {
		index := slices.IndexFunc(s.SecurityGroups, secGroupFn)
		Expect(index).NotTo(BeNumerically("==", -1))
		Expect(slices.IndexFunc(s.SecurityGroups[index+1:], secGroupFn)).To(BeNumerically("==", -1))
	}
}

func (t *testDriver) assertNoServerSecGroup(groupName string) {
	for _, s := range t.servers {
		Expect(slices.ContainsFunc(s.SecurityGroups, func(m map[string]any) bool {
			return m["name"] == groupName
		})).To(BeFalse())
	}
}

func (t *testDriver) testErrors(run func() error, entries ...any) {
	params := []any{
		func(message string, before func()) {
			When(message+" fails", func() {
				BeforeEach(func() {
					before()
				})

				It("should return an error", func() {
					Expect(run()).NotTo(Succeed())
				})
			})
		},
	}

	DescribeTableSubtree("", append(params, entries...)...)
}

func newComputeV2ErrEntry() TableEntry {
	return Entry("", "NewComputeV2", func() {
		rhos.NewComputeV2 = func(_ *gophercloud.ProviderClient, _ gophercloud.EndpointOpts) (*gophercloud.ServiceClient, error) {
			return nil, errors.New("compute client error")
		}
	})
}

func newNetworkV2ErrEntry() TableEntry {
	return Entry("", "NewNetworkV2", func() {
		rhos.NewNetworkV2 = func(_ *gophercloud.ProviderClient, _ gophercloud.EndpointOpts) (*gophercloud.ServiceClient, error) {
			return nil, errors.New("network client error")
		}
	})
}

func createSecurityGroupErrEntry() TableEntry {
	return Entry("", "CreateSecurityGroup", func() {
		rhos.CreateSecurityGroup = func(_ context.Context, _ *gophercloud.ServiceClient, _ secgroups.CreateOptsBuilder) (
			*secgroups.SecurityGroup, error,
		) {
			return nil, errors.New("create security group error")
		}
	})
}

func extractSecurityGroupsErrEntry() TableEntry {
	return Entry("", "ExtractSecurityGroups", func() {
		rhos.ExtractSecurityGroups = func(_ pagination.Page) ([]secgroups.SecurityGroup, error) {
			return nil, errors.New("extract security groups error")
		}
	})
}

func extractServersErrEntry() TableEntry {
	return Entry("", "ExtractServers", func() {
		rhos.ExtractServers = func(page pagination.Page) ([]servers.Server, error) {
			return nil, errors.New("extract servers error")
		}
	})
}

func addServerErrEntry() TableEntry {
	return Entry("", "AddServer", func() {
		rhos.AddServer = func(_ context.Context, _ *gophercloud.ServiceClient, _, _ string) error {
			return errors.New("add server error")
		}
	})
}

func deleteSecurityGroupErrEntry() TableEntry {
	return Entry("", "DeleteSecurityGroup", func() {
		rhos.DeleteSecurityGroup = func(_ context.Context, _ *gophercloud.ServiceClient, _ string) secgroups.DeleteResult {
			result := secgroups.DeleteResult{}
			result.Err = errors.New("delete security group error")

			return result
		}
	})
}

func removeServerErrEntry() TableEntry {
	return Entry("", "RemoveServer", func() {
		rhos.RemoveServer = func(_ context.Context, _ *gophercloud.ServiceClient, _, _ string) error {
			return errors.New("remove server error")
		}
	})
}

type EmptyPage struct{}

func (p EmptyPage) NextPageURL() (string, error) {
	return "", nil
}

func (p EmptyPage) IsEmpty() (bool, error) {
	return true, nil
}

func (p EmptyPage) GetBody() any {
	return nil
}

type SecGroupPage struct {
	EmptyPage
	secGroups []secgroups.SecurityGroup
}

type ServerPage struct {
	EmptyPage
	servers []servers.Server
}

type SubnetParam struct {
	Filter SubnetFilter `json:"filter"`
}
type SubnetFilter struct {
	Name string `json:"name,omitempty"`
	Tags string `json:"tags,omitempty"`
}
