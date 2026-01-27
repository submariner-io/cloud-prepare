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
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	"github.com/submariner-io/cloud-prepare/pkg/rhos"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Cloud", func() {
	Context("OpenPorts", testOpenPorts)
	Context("ClosePorts", testClosePorts)
})

func testOpenPorts() {
	t := newRHOSTestDriver()

	var ports []api.PortSpec

	BeforeEach(func() {
		ports = []api.PortSpec{
			{
				Port:     4500,
				Protocol: "UDP",
			},
			{
				Port:     500,
				Protocol: "UDP",
			},
		}
	})

	When("the internal security group does not exist", func() {
		It("should create the security group and open internal ports", func() {
			Expect(t.cloud.OpenPorts(ctx, ports, reporter.Stdout())).To(Succeed())

			Expect(t.securityGroupsCreated).To(ContainElement(internalSecurityGroup))

			for _, port := range ports {
				t.assertRuleCreated(rhos.InternalSecurityGroupSuffix, port)
			}

			t.assertServerSecGroup(internalSecurityGroup)
		})
	})

	When("the internal security group already exists", func() {
		BeforeEach(func() {
			t.existingSecurityGroups = []secgroups.SecurityGroup{
				{
					Name: internalSecurityGroup,
					ID:   internalSecurityGroup,
				},
			}
		})

		It("should not recreate it but should add servers to it", func() {
			Expect(t.cloud.OpenPorts(ctx, ports, reporter.Stdout())).To(Succeed())

			Expect(t.securityGroupsCreated).To(BeEmpty())
			t.assertServerSecGroup(internalSecurityGroup)
		})
	})

	t.testErrors(func() error { return t.cloud.OpenPorts(ctx, ports, reporter.Stdout()) },
		newComputeV2ErrEntry(),
		newNetworkV2ErrEntry(),
		createSecurityGroupErrEntry(),
		extractSecurityGroupsErrEntry(),
		extractServersErrEntry(),
		addServerErrEntry())
}

func testClosePorts() {
	t := newRHOSTestDriver()

	BeforeEach(func() {
		t.existingSecurityGroups = []secgroups.SecurityGroup{
			{
				Name: internalSecurityGroup,
				ID:   internalSecurityGroup,
			},
		}

		t.servers[nodeName1].SecurityGroups = []map[string]any{
			{"name": internalSecurityGroup},
		}
		t.servers[nodeName2].SecurityGroups = []map[string]any{
			{"name": internalSecurityGroup},
		}
	})

	It("should remove the security group from servers and delete it", func() {
		Expect(t.cloud.ClosePorts(ctx, reporter.Stdout())).To(Succeed())

		t.assertNoServerSecGroup(internalSecurityGroup)
	})

	t.testErrors(func() error { return t.cloud.ClosePorts(ctx, reporter.Stdout()) },
		newComputeV2ErrEntry(),
		deleteSecurityGroupErrEntry(),
		extractSecurityGroupsErrEntry(),
		extractServersErrEntry(),
		removeServerErrEntry())
}

type rhosTestDriver struct {
	*testDriver
	cloud api.Cloud
}

func newRHOSTestDriver() *rhosTestDriver {
	t := &rhosTestDriver{testDriver: newTestDriver()}

	JustBeforeEach(func() {
		info := rhos.CloudInfo{
			Client:    &gophercloud.ProviderClient{},
			InfraID:   testInfraID,
			Region:    testRegion,
			K8sClient: k8s.NewInterface(k8sfake.NewClientset()),
		}

		t.cloud = rhos.NewCloud(info)
	})

	return t
}
