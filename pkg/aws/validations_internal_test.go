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
package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

var _ = Describe("Validations", func() {
	Context("ValidatePeeringPrerequisites", testValidatePeeringPrerequisites)
	Context("checkVpcOverlap", testCheckVpcOverlap)
	Context("DeterminePermissionError", testDeterminePermissionError)
})

func testDeterminePermissionError() {
	When("called with a nil error", func() {
		It("should return nil", func() {
			err := determinePermissionError(nil, "")
			Expect(err).To(BeNil())
		})
	})
	When("called with an AWS DryRunOperation error", func() {
		It("should return nil", func() {
			err := smithy.GenericAPIError{
				Code:    "DryRunOperation",
				Message: "DryRunOperation",
				Fault:   1,
			}
			operation := "check"
			result := determinePermissionError(&err, operation)
			Expect(result).To(BeNil())
		})
	})
	When("called with an AWS UnauthorizedOperation error", func() {
		It("should return an appropriate error", func() {
			err := smithy.GenericAPIError{
				Code:    "UnauthorizedOperation",
				Message: "UnauthorizedOperation",
				Fault:   1,
			}
			operation := "check"
			result := determinePermissionError(&err, operation)
			Expect(result).To(
				MatchError(
					MatchRegexp("no permission to " + operation),
				),
			)
		})
	})
	When("called with a general error", func() {
		It("should return an appropriate error", func() {
			err := smithy.GenericAPIError{
				Code:    "Generic Error",
				Message: "Generic Error",
				Fault:   1,
			}
			operation := ""
			result := determinePermissionError(&err, operation)
			Expect(result).To(
				MatchError(
					MatchRegexp("error while checking permissions for " + operation),
				),
			)
		})
	})
}

func testValidatePeeringPrerequisites() {
	cloudA := newCloudTestDriver(infraID, region)
	cloudB := newCloudTestDriver(targetInfraID, targetRegion)

	When("trying to retrieve a missing source VPC", func() {
		It("should return an error", func() {
			cloudA.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(
					&ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{},
					}, nil)
			awsCloudA, ok := cloudA.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			awsCloudB, ok := cloudB.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			err := awsCloudA.validatePeeringPrerequisites(awsCloudB, api.NewLoggingReporter())

			Expect(err).To(HaveOccurred())
		})
	})
	When("trying to retrieve a missing target VPC", func() {
		It("should return an error", func() {
			cloudA.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-a", "1.2.3.4/16"), nil)
			cloudB.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(
					&ec2.DescribeVpcsOutput{
						Vpcs: []types.Vpc{},
					}, nil)
			awsCloudA, ok := cloudA.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			awsCloudB, ok := cloudB.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			err := awsCloudA.validatePeeringPrerequisites(awsCloudB, api.NewLoggingReporter())

			Expect(err).To(HaveOccurred())
		})
	})
	When("an invalid CIDR Block is provided", func() {
		It("should return an error", func() {
			cloudA.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-a", "make it fail"), nil)
			cloudB.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-b", "1.2.3.4/16"), nil)
			awsCloudA, ok := cloudA.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			awsCloudB, ok := cloudB.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			err := awsCloudA.validatePeeringPrerequisites(awsCloudB, api.NewLoggingReporter())

			Expect(err).To(HaveOccurred())
		})
	})
	When("the source and target VPCs overlap", func() {
		It("should return an error", func() {
			cloudA.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-a", "1.2.3.4/16"), nil)
			cloudB.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-b", "1.2.3.4/16"), nil)
			awsCloudA, ok := cloudA.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			awsCloudB, ok := cloudB.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			err := awsCloudA.validatePeeringPrerequisites(awsCloudB, api.NewLoggingReporter())

			Expect(err).To(HaveOccurred())
		})
	})
	When("the requirements are met", func() {
		It("should not return an error", func() {
			cloudA.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-a", "10.0.0.0/16"), nil)
			cloudB.awsClient.EXPECT().DescribeVpcs(context.TODO(), gomock.Any()).
				Return(getVpcOutputFor("vpc-b", "10.1.0.0/16"), nil)
			awsCloudA, ok := cloudA.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			awsCloudB, ok := cloudB.cloud.(*awsCloud)
			Expect(ok).To(BeTrue())
			err := awsCloudA.validatePeeringPrerequisites(awsCloudB, api.NewLoggingReporter())

			Expect(err).ToNot(HaveOccurred())
		})
	})
}

func testCheckVpcOverlap() {
	When("an invalid CIDR block is provided", func() {
		var vpcA, vpcB *types.Vpc
		It("should fail when the left CIDR block is invalid", func() {
			netA := "1.2.3.4/-1"
			netB := "10.0.0.0/16"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, err := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeFalse())
			Expect(err).NotTo(BeNil())
		})
		It("should fail when the right CIDR block is invalid", func() {
			netA := "10.0.0.0/16"
			netB := "1.2.3.4/-1"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, err := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeFalse())
			Expect(err).NotTo(BeNil())
		})
	})
	When("valid CIDR blocks are provided", func() {
		var vpcA, vpcB *types.Vpc
		It("should return false for non overlapping subnets", func() {
			netA := "10.0.0.0/16"
			netB := "10.1.0.0/16"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, _ := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeFalse())
		})
	})
	When("CIDR blocks overlap", func() {
		var vpcA, vpcB *types.Vpc
		It("should fail when the same CIDR blocks are provided", func() {
			netA := "10.0.0.0/16"
			netB := "10.0.0.0/16"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, _ := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeTrue())
		})
		It("should fail when different overlapping blocks are provided", func() {
			netA := "10.0.0.0/12"
			netB := "10.1.0.0/12"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, _ := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeTrue())
		})
		It("should fail when one CIDR block is an IP part of the other CIDR block", func() {
			netA := "192.168.0.1/32"
			netB := "0.0.0.0/0"
			vpcA = &types.Vpc{CidrBlock: &netA}
			vpcB = &types.Vpc{CidrBlock: &netB}
			response, _ := checkVpcOverlap(vpcA, vpcB)
			Expect(response).To(BeTrue())
		})
	})
}
