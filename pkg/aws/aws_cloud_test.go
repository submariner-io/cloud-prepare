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

package aws_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/aws"
)

var _ = Describe("Cloud", func() {
	Describe("OpenPorts", testOpenPorts)
	Describe("ClosePorts", testClosePorts)
})

func testOpenPorts() {
	t := newCloudTestDriver()

	var retError error

	JustBeforeEach(func() {
		t.expectDescribeVpcs(t.vpcID)
		t.expectDescribePublicSubnets(t.subnets...)

		retError = t.cloud.OpenPorts([]api.PortSpec{
			{
				Port:     100,
				Protocol: "TCP",
			},
			{
				Port:     200,
				Protocol: "UDP",
			},
		}, reporter.Stdout())
	})

	When("on success", func() {
		BeforeEach(func() {
			Expect(retError).To(Succeed())

			t.expectValidateAuthorizeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroups(masterSGName, masterGroupID)

			t.expectAuthorizeSecurityGroupIngress(workerGroupID, newClusterSGRule(workerGroupID, 100, "TCP"))
			t.expectAuthorizeSecurityGroupIngress(workerGroupID, newClusterSGRule(masterGroupID, 100, "TCP"))
			t.expectAuthorizeSecurityGroupIngress(masterGroupID, newClusterSGRule(workerGroupID, 100, "TCP"))

			t.expectAuthorizeSecurityGroupIngress(workerGroupID, newClusterSGRule(workerGroupID, 200, "UDP"))
			t.expectAuthorizeSecurityGroupIngress(workerGroupID, newClusterSGRule(masterGroupID, 200, "UDP"))
			t.expectAuthorizeSecurityGroupIngress(masterGroupID, newClusterSGRule(workerGroupID, 200, "UDP"))
		})

		It("should authorize the appropriate security groups ingress", func() {
			Expect(retError).To(Succeed())
		})
	})

	When("the infra ID VPC does not exist", func() {
		BeforeEach(func() {
			t.vpcID = ""
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})

	When("authorize security group ingress validation fails", func() {
		BeforeEach(func() {
			t.expectDescribeVpcs(vpcID)
			t.expectDescribePublicSubnets(t.subnets...)
			t.expectValidateAuthorizeSecurityGroupIngress(errors.New("mock error"))
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})

	When("retrieval of security groups fails", func() {
		BeforeEach(func() {
			t.expectValidateAuthorizeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroupsFailure(masterSGName, errors.New("mock error"))
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})
}

func testClosePorts() {
	t := newCloudTestDriver()

	var retError error

	JustBeforeEach(func() {
		t.expectDescribeVpcs(t.vpcID)
		t.expectDescribePublicSubnets(t.subnets...)
		t.expectDescribePublicSubnetsSigs(t.subnets...)

		retError = t.cloud.ClosePorts(reporter.Stdout())
	})

	Context("on success", func() {
		BeforeEach(func() {
			t.expectValidateRevokeSecurityGroupIngress(nil)

			ipPerm := newIPPermission(internalTraffic + " from X to Y")
			t.expectDescribeSecurityGroups(masterSGName, masterGroupID, ipPerm, newIPPermission("other"))

			t.expectRevokeSecurityGroupIngress(masterGroupID, ipPerm)
		})

		It("should revoke the appropriate security groups ingress", func() {
			Expect(retError).To(Succeed())
		})
	})

	When("the infra ID VPC does not exist", func() {
		BeforeEach(func() {
			t.vpcID = ""
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})

	When("authorize security group ingress validation fails", func() {
		BeforeEach(func() {
			t.expectValidateRevokeSecurityGroupIngress(errors.New("mock error"))
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})

	When("retrieval of security groups fails", func() {
		BeforeEach(func() {
			t.expectValidateRevokeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroupsFailure(masterSGName, errors.New("mock error"))
		})

		It("should return an error", func() {
			Expect(retError).To(HaveOccurred())
		})
	})
}

type cloudTestDriver struct {
	fakeAWSClientBase
	cloud api.Cloud
}

func newCloudTestDriver() *cloudTestDriver {
	t := &cloudTestDriver{}

	BeforeEach(func() {
		t.beforeEach()

		t.cloud = aws.NewCloud(t.awsClient, infraID, region)

		t.expectDescribeSecurityGroups(workerSGName, workerGroupID)
	})

	AfterEach(t.afterEach)

	return t
}
