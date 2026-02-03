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
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/aws"
	"k8s.io/utils/ptr"
)

var _ = Describe("Cloud", func() {
	Describe("OpenPorts", testOpenPorts)
	Describe("ClosePorts", testClosePorts)
})

func testOpenPorts() {
	t := newCloudTestDriver()

	JustBeforeEach(func() {
		t.expectDescribeVpcs()
		t.expectDescribeVpcsSigs()
		t.expectDescribePublicSubnets(t.existingSubnets...)
		t.expectDescribeSecurityGroups(t.workerSGName, t.workerGroupID)
	})

	doOpenPorts := func(ctx context.Context) error {
		return t.cloud.OpenPorts(ctx, []api.PortSpec{
			{
				Port:     100,
				Protocol: "TCP",
			},
			{
				Port:     200,
				Protocol: "UDP",
			},
		}, reporter.Stdout())
	}

	When("on success", func() {
		JustBeforeEach(func() {
			t.expectValidateAuthorizeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroups(t.masterSGName, t.masterGroupID)

			t.expectAuthorizeSecurityGroupIngress(t.workerGroupID, newClusterSGRule(t.workerGroupID, 100, "TCP"))
			t.expectAuthorizeSecurityGroupIngress(t.workerGroupID, newClusterSGRule(t.masterGroupID, 100, "TCP"))
			t.expectAuthorizeSecurityGroupIngress(t.masterGroupID, newClusterSGRule(t.workerGroupID, 100, "TCP"))

			t.expectAuthorizeSecurityGroupIngress(t.workerGroupID, newClusterSGRule(t.workerGroupID, 200, "UDP"))
			t.expectAuthorizeSecurityGroupIngress(t.workerGroupID, newClusterSGRule(t.masterGroupID, 200, "UDP"))
			t.expectAuthorizeSecurityGroupIngress(t.masterGroupID, newClusterSGRule(t.workerGroupID, 200, "UDP"))
		})

		It("should authorize the appropriate security groups ingress", func(ctx SpecContext) {
			Expect(doOpenPorts(ctx)).To(Succeed())
		})

		Context("WithWorkerSecurityGroup", func() {
			BeforeEach(func() {
				t.workerGroupID = customWorkerGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithWorkerSecurityGroup(t.workerGroupID))
			})

			It("should authorize the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doOpenPorts(ctx)).To(Succeed())
			})
		})

		Context("WithControlPlaneSecurityGroup", func() {
			BeforeEach(func() {
				t.masterGroupID = customMasterGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithControlPlaneSecurityGroup(t.masterGroupID))
			})

			It("should authorize the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doOpenPorts(ctx)).To(Succeed())
			})
		})

		Context("WithPublicSubnetList", func() {
			BeforeEach(func() {
				t.workerSGName = infraID + "-node"
				t.masterSGName = infraID + "-controlplane"
				t.existingSubnets = []types.Subnet{}
				t.cloudOptions = append(t.cloudOptions, aws.WithPublicSubnetList([]string{customSubnet}))

				t.expectDescribePublicSubnetsByID(customSubnet, types.Subnet{
					SubnetId: ptr.To(customSubnet),
					Tags: []types.Tag{
						{
							Key:   ptr.To("Name"),
							Value: ptr.To(infraID + "-x-subnet-public-" + region + "-end"),
						},
					},
				})
			})

			It("should authorize the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doOpenPorts(ctx)).To(Succeed())
			})
		})

		Context("WithVPCName and WithWorkerSecurityGroup and WithControlPlaneSecurityGroup", func() {
			BeforeEach(func() {
				t.existingVpcs = []types.Vpc{}
				t.vpcID = customVPC
				t.masterGroupID = customMasterGroup
				t.workerGroupID = customWorkerGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithVPCName(t.vpcID), aws.WithControlPlaneSecurityGroup(t.masterGroupID),
					aws.WithWorkerSecurityGroup(t.workerGroupID))
			})

			It("should authorize the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doOpenPorts(ctx)).To(Succeed())
			})
		})
	})

	When("the infra ID VPC does not exist", func() {
		BeforeEach(func() {
			t.existingVpcs = []types.Vpc{}
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doOpenPorts(ctx)).NotTo(Succeed())
		})
	})

	Context("WithVPCName without worker and control plane security groups specified", func() {
		BeforeEach(func() {
			t.existingVpcs = []types.Vpc{}
			t.vpcID = customVPC
			t.cloudOptions = append(t.cloudOptions, aws.WithVPCName(t.vpcID))
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doOpenPorts(ctx)).NotTo(Succeed())
		})
	})

	When("authorize security group ingress validation fails", func() {
		BeforeEach(func() {
			t.expectDescribePublicSubnets(t.existingSubnets...)
			t.expectValidateAuthorizeSecurityGroupIngress(errors.New("mock error"))
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doOpenPorts(ctx)).NotTo(Succeed())
		})
	})

	When("retrieval of security groups fails", func() {
		BeforeEach(func() {
			t.expectValidateAuthorizeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroupsFailure(t.masterSGName, errors.New("mock error"))
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doOpenPorts(ctx)).NotTo(Succeed())
		})
	})
}

func testClosePorts() {
	ipPerm := newIPPermission(internalTraffic + " from X to Y")

	t := newCloudTestDriver()

	JustBeforeEach(func() {
		t.expectDescribeVpcs()
		t.expectDescribeVpcsSigs()
		t.expectDescribePublicSubnets(t.existingSubnets...)
		t.expectDescribePublicSubnetsSigs(t.existingSubnets...)
		t.expectDescribeSecurityGroups(t.workerSGName, t.workerGroupID, ipPerm)
	})

	doClosePorts := func(ctx context.Context) error {
		return t.cloud.ClosePorts(ctx, reporter.Stdout())
	}

	Context("on success", func() {
		JustBeforeEach(func() {
			t.expectDescribeSecurityGroups(t.workerSGName, t.workerGroupID)
			t.expectValidateRevokeSecurityGroupIngress(nil)

			t.expectDescribeSecurityGroups(t.masterSGName, t.masterGroupID, ipPerm, newIPPermission("other"))

			t.expectRevokeSecurityGroupIngress(t.masterGroupID, ipPerm)
			t.expectRevokeSecurityGroupIngress(t.workerGroupID, ipPerm)
		})

		It("should revoke the appropriate security groups ingress", func(ctx SpecContext) {
			Expect(doClosePorts(ctx)).To(Succeed())
		})

		Context("WithWorkerSecurityGroup", func() {
			BeforeEach(func() {
				t.workerGroupID = customWorkerGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithWorkerSecurityGroup(t.workerGroupID))

				t.expectDescribeSecurityGroupsByID(t.workerGroupID, ipPerm)
			})

			It("should revoke the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doClosePorts(ctx)).To(Succeed())
			})
		})

		Context("WithControlPlaneSecurityGroup", func() {
			BeforeEach(func() {
				t.masterGroupID = customMasterGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithControlPlaneSecurityGroup(t.masterGroupID))

				t.expectDescribeSecurityGroupsByID(t.masterGroupID, ipPerm)
			})

			It("should revoke the appropriate security groups ingress", func(ctx SpecContext) {
				Expect(doClosePorts(ctx)).To(Succeed())
			})
		})
	})

	When("the infra ID VPC does not exist", func() {
		BeforeEach(func() {
			t.existingVpcs = []types.Vpc{}
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doClosePorts(ctx)).To(HaveOccurred())
		})
	})

	When("authorize security group ingress validation fails", func() {
		BeforeEach(func() {
			t.expectValidateRevokeSecurityGroupIngress(errors.New("mock error"))
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doClosePorts(ctx)).To(HaveOccurred())
		})
	})

	When("retrieval of security groups fails", func() {
		BeforeEach(func() {
			t.expectValidateRevokeSecurityGroupIngress(nil)
			t.expectDescribeSecurityGroupsFailure(t.masterSGName, errors.New("mock error"))
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doClosePorts(ctx)).To(HaveOccurred())
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
	})

	JustBeforeEach(func() {
		t.cloud = aws.NewCloud(t.awsClient, infraID, region, t.cloudOptions...)
	})

	AfterEach(t.afterEach)

	return t
}
