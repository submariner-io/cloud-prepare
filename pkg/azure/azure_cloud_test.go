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

package azure_test

import (
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v10"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/azure"
	"k8s.io/utils/ptr"
)

const internalSecurityGroupName = testInfraID + azure.InternalSecurityGroupSuffix

var _ = Describe("Cloud", func() {
	internalSecurityGroupPath := securityGroupPath(internalSecurityGroupName)

	t := newTestDriver()

	BeforeEach(func() {
		t.httpGetResponses[internalSecurityGroupPath] = &armnetwork.SecurityGroup{
			Name: ptr.To(internalSecurityGroupName),
		}
	})

	Specify("should open and close ports correctly", func(ctx SpecContext) {
		cloud := azure.NewCloud(&t.cloudInfo)

		port := api.PortSpec{
			Port:     80,
			Protocol: string(armnetwork.SecurityRuleProtocolTCP),
		}

		Expect(cloud.OpenPorts(ctx, []api.PortSpec{port}, reporter.Stdout())).To(Succeed())

		var securityGroup armnetwork.SecurityGroup
		t.assertPutRequest(internalSecurityGroupPath, &securityGroup)

		Expect(securityGroup.Properties.SecurityRules).To(ConsistOf(securityRuleMatchers(port, azure.InternalSecurityRulePrefix)))

		clear(t.httpPutRequests)

		By("Open ports again - should be a noop")

		t.httpGetResponses[internalSecurityGroupPath] = &securityGroup

		Expect(cloud.OpenPorts(ctx, []api.PortSpec{port}, reporter.Stdout())).To(Succeed())
		t.assertNoPutRequest(internalSecurityGroupPath)

		By("Close ports")

		otherRule := &armnetwork.SecurityRule{
			Name: ptr.To("should-not-be-removed"),
		}
		securityGroup.Properties.SecurityRules = append(securityGroup.Properties.SecurityRules, otherRule)

		Expect(cloud.ClosePorts(ctx, reporter.Stdout())).To(Succeed())
		t.assertPutRequest(internalSecurityGroupPath, &securityGroup)
		Expect(securityGroup.Properties.SecurityRules).To(ConsistOf(Satisfy(func(r *armnetwork.SecurityRule) bool {
			return ptr.Deref(r.Name, "") == *otherRule.Name
		})))
	})

	When("security group retrieval fails", func() {
		Specify("OpenPorts should return an error", func(ctx SpecContext) {
			t.httpGetResponses[internalSecurityGroupPath] = http.StatusUnauthorized

			cloud := azure.NewCloud(&t.cloudInfo)
			Expect(cloud.OpenPorts(ctx, []api.PortSpec{{Port: 80, Protocol: string(armnetwork.SecurityRuleProtocolTCP)}},
				reporter.Stdout())).NotTo(Succeed())
		})
	})

	When("security group creation fails", func() {
		Specify("OpenPorts should return an error", func(ctx SpecContext) {
			t.httpPutRespCodes[internalSecurityGroupPath] = ptr.To(http.StatusUnauthorized)

			cloud := azure.NewCloud(&t.cloudInfo)
			Expect(cloud.OpenPorts(ctx, []api.PortSpec{{Port: 80, Protocol: string(armnetwork.SecurityRuleProtocolTCP)}},
				reporter.Stdout())).NotTo(Succeed())
		})
	})
})
