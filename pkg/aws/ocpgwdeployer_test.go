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
	"github.com/stretchr/testify/mock"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/aws"
	ocpFake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"
)

var _ = Describe("OCP GatewayDeployer", func() {
	Context("on Deploy", testDeploy)
	Context("on Cleanup", testCleanup)
})

func testDeploy() {
	t := newGatewayDeployerTestDriver()

	var deployCall *mock.Call

	JustBeforeEach(func() {
		deployCall = t.msDeployer.EXPECT().Deploy(mock.Anything, mock.Anything).RunAndReturn(machineSetFn(&t.machineSets)).Call

		t.expectDescribePublicSubnets(t.existingSubnets...)
		t.setupExpectedSubnetInstanceTypeOfferings(t.existingSubnets...)
	})

	When("on success", func() {
		BeforeEach(func() {
			t.expectDeployValidations(true)

			t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(100, "TCP"))
			t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(200, "UDP"))
		})

		JustBeforeEach(func() {
			deployCall.Times(t.numGateways)

			for i := range t.expectedSubnetsTagged {
				t.expectCreateGatewayTags(*t.expectedSubnetsTagged[i].SubnetId)
			}
		})

		t.testDeploySuccess("", "")

		Context("and the gateway security group doesn't initially exist", func() {
			BeforeEach(func() {
				t.gatewayGroupID = ""
				t.expectCreateSecurityGroup(gatewaySGName, gatewayGroupID)
			})

			t.testDeploySuccess("should create it and", "")
		})

		Context("and the first subnet doesn't have an instance type offering", func() {
			BeforeEach(func() {
				t.expectedSubnets = []types.Subnet{t.existingSubnets[1]}
				t.zonesWithInstanceTypeOfferings = set.New(availabilityZone2)
			})

			t.testDeploySuccess("", "")
		})

		Context("and the deploying subnet is already tagged", func() {
			BeforeEach(func() {
				t.expectedSubnetsTagged = []types.Subnet{}
				t.existingSubnets[0].Tags = append(t.existingSubnets[0].Tags, types.Tag{
					Key:   ptr.To("submariner.io/gateway"),
					Value: ptr.To(""),
				})
			})

			t.testDeploySuccess("", " without retagging it")
		})

		Context("and a desired instance type is not provided", func() {
			BeforeEach(func() {
				t.instanceType = ""
			})

			t.testDeploySuccess("should select an instance type and", "")
		})

		Context("WithWorkerSecurityGroup", func() {
			BeforeEach(func() {
				t.workerGroupID = customWorkerGroup
				t.workerSGName = t.workerGroupID + "-name"
				t.cloudOptions = append(t.cloudOptions, aws.WithWorkerSecurityGroup(t.workerGroupID))

				t.expectDescribeSecurityGroupsByID(t.workerGroupID)
			})

			t.testDeploySuccess("", "")
		})

		Context("WithVPCName", func() {
			BeforeEach(func() {
				t.existingVpcs = []types.Vpc{}
				t.vpcID = customVPC
				t.masterGroupID = customMasterGroup
				t.workerGroupID = customWorkerGroup
				t.cloudOptions = append(t.cloudOptions, aws.WithVPCName(t.vpcID), aws.WithControlPlaneSecurityGroup(t.masterGroupID),
					aws.WithWorkerSecurityGroup(t.workerGroupID))
				t.workerSGName = customWorkerGroup + "-name"

				t.expectDescribeSecurityGroupsByID(t.workerGroupID)
			})

			t.testDeploySuccess("", "")
		})

		Context("WithPublicSubnetList", func() {
			BeforeEach(func() {
				t.existingSubnets = []types.Subnet{}
				t.cloudOptions = append(t.cloudOptions, aws.WithPublicSubnetList([]string{customSubnet}))

				t.expectedSubnets = []types.Subnet{newSubnet(availabilityZone1, customSubnet)}
				t.expectDescribePublicSubnetsByID(customSubnet, t.expectedSubnets[0])

				t.zonesWithInstanceTypeOfferings = set.New(availabilityZone1)
			})

			JustBeforeEach(func() {
				t.setupExpectedSubnetInstanceTypeOfferings(t.expectedSubnets...)
			})

			t.testDeploySuccess("", "")
		})
	})

	Context("", func() {
		JustBeforeEach(func() {
			deployCall.Maybe()
		})

		BeforeEach(func() {
			t.expectDeployValidations(false)
		})

		When("the infra ID VPC does not exist", func() {
			BeforeEach(func() {
				t.existingVpcs = []types.Vpc{}
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doDeploy(ctx)).To(HaveOccurred())
			})
		})

		When("the retrieval of public subnets fails", func() {
			BeforeEach(func() {
				t.describeSubnetsErr = errors.New("mock error")
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doDeploy(ctx)).To(HaveOccurred())
			})
		})

		When("tagging a public subnet fails", func() {
			BeforeEach(func() {
				t.createTagsErr = errors.New("mock error")
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(100, "TCP"))
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(200, "UDP"))
			})

			JustBeforeEach(func() {
				t.expectCreateGatewayTags(*t.expectedSubnetsTagged[0].SubnetId)
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doDeploy(ctx)).To(HaveOccurred())
			})
		})

		When("the creation of a security group fails", func() {
			BeforeEach(func() {
				t.authorizeSecurityGroupIngressErr = errors.New("mock error")
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(100, "TCP"))
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doDeploy(ctx)).To(HaveOccurred())
			})
		})

		When("there's an insufficient number of public subnets", func() {
			BeforeEach(func() {
				t.existingSubnets = []types.Subnet{}
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doDeploy(ctx)).To(HaveOccurred())
			})
		})
	})
}

func testCleanup() {
	t := newGatewayDeployerTestDriver()

	JustBeforeEach(func() {
		t.expectDescribeGatewaySubnets(t.existingSubnets...)
	})

	When("on success", func() {
		JustBeforeEach(func() {
			t.expectCleanupValidations(true)
			t.msDeployer.EXPECT().Delete(mock.Anything, mock.Anything).RunAndReturn(machineSetFn(&t.machineSets)).
				Times(len(t.existingSubnets))
			t.expectDeleteSecurityGroup(gatewayGroupID)

			for i := range t.existingSubnets {
				t.expectDeleteGatewayTags(*t.existingSubnets[i].SubnetId)
			}
		})

		t.testCleanupSuccess()

		Context("WithPublicSubnetList", func() {
			BeforeEach(func() {
				t.existingSubnets = []types.Subnet{newSubnet(availabilityZone1, customSubnet)}
				t.cloudOptions = append(t.cloudOptions, aws.WithPublicSubnetList([]string{customSubnet}))

				t.expectDescribePublicSubnetsByID(customSubnet, t.existingSubnets[0])
			})

			t.testCleanupSuccess()
		})
	})

	Context("", func() {
		BeforeEach(func() {
			t.expectCleanupValidations(false)
		})

		When("the infra ID VPC does not exist", func() {
			BeforeEach(func() {
				t.existingVpcs = []types.Vpc{}
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doCleanup(ctx)).To(HaveOccurred())
			})
		})

		When("the retrieval of public subnets fails", func() {
			BeforeEach(func() {
				t.describeSubnetsErr = errors.New("mock error")
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(t.doCleanup(ctx)).To(HaveOccurred())
			})
		})
	})
}

type gatewayDeployerTestDriver struct {
	fakeAWSClientBase
	numGateways                    int
	instanceType                   string
	expectedSubnets                []types.Subnet
	expectedSubnetsDeployed        []types.Subnet
	expectedSubnetsTagged          []types.Subnet
	gatewayGroupID                 string
	zonesWithInstanceTypeOfferings set.Set[string]
	machineSets                    map[string]*unstructured.Unstructured
	msDeployer                     *ocpFake.MockMachineSetDeployer
	gwDeployer                     api.GatewayDeployer
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{}

	BeforeEach(func() {
		t.beforeEach()

		t.msDeployer = ocpFake.NewMockMachineSetDeployer(GinkgoT())
		t.numGateways = 1
		t.instanceType = "test-instance-type"
		t.expectedSubnets = []types.Subnet{t.existingSubnets[0]}
		t.expectedSubnetsDeployed = nil
		t.expectedSubnetsTagged = nil
		t.gatewayGroupID = gatewayGroupID
		t.zonesWithInstanceTypeOfferings = nil
	})

	JustBeforeEach(func() {
		if t.zonesWithInstanceTypeOfferings == nil {
			t.zonesWithInstanceTypeOfferings = set.New[string]()
			for i := range t.existingSubnets {
				t.zonesWithInstanceTypeOfferings.Insert(*t.existingSubnets[i].AvailabilityZone)
			}
		}

		if t.expectedSubnetsDeployed == nil {
			t.expectedSubnetsDeployed = t.expectedSubnets
		}

		if t.expectedSubnetsTagged == nil {
			t.expectedSubnetsTagged = t.expectedSubnets
		}

		t.expectDescribeVpcs()
		t.expectDescribeSecurityGroups(gatewaySGName, t.gatewayGroupID)
		t.expectDescribeInstances(instanceImageID)
		t.expectDescribeSecurityGroups(t.workerSGName, t.workerGroupID)
		t.expectDescribePublicSubnets(t.existingSubnets...)
		t.expectDescribeVpcsSigs()
		t.expectDescribePublicSubnetsSigs(t.existingSubnets...)

		var err error

		t.gwDeployer, err = aws.NewOcpGatewayDeployer(aws.NewCloud(t.awsClient, infraID, region, t.cloudOptions...),
			t.msDeployer, t.instanceType)
		Expect(err).To(Succeed())
	})

	return t
}

func (t *gatewayDeployerTestDriver) expectedInstanceType() string {
	if t.instanceType != "" {
		return t.instanceType
	}

	return aws.PreferredInstances[0]
}

func (t *gatewayDeployerTestDriver) doDeploy(ctx context.Context) error {
	return t.gwDeployer.Deploy(ctx, api.GatewayDeployInput{
		Gateways: t.numGateways,
		PublicPorts: []api.PortSpec{
			{
				Port:     100,
				Protocol: "TCP",
			},
			{
				Port:     200,
				Protocol: "UDP",
			},
		},
	}, reporter.Stdout())
}

func (t *gatewayDeployerTestDriver) doCleanup(ctx context.Context) error {
	return t.gwDeployer.Cleanup(ctx, reporter.Stdout())
}

func (t *gatewayDeployerTestDriver) expectDeployValidations(enforce bool) {
	calls := []*mock.Call{
		t.expectValidateCreateSecurityGroup(),
		t.expectValidateAuthorizeSecurityGroupIngress(nil),
		t.expectValidateDescribeInstanceTypeOfferings(),
		t.expectValidateCreateTags(),
	}

	for _, c := range calls {
		if !enforce {
			c.Maybe()
		}
	}
}

func (t *gatewayDeployerTestDriver) expectCleanupValidations(enforce bool) {
	calls := []*mock.Call{
		t.expectValidateDeleteSecurityGroup(),
		t.expectValidateDeleteTags(),
	}

	for _, c := range calls {
		if !enforce {
			c.Maybe()
		}
	}
}

func (t *gatewayDeployerTestDriver) testDeploySuccess(msgPrefix, msgSuffix string) {
	var msg string
	if msgPrefix != "" {
		msg = msgPrefix
	} else {
		msg = "should"
	}

	It(msg+" deploy the correct gateway node machine sets"+msgSuffix, func(ctx SpecContext) {
		Expect(t.doDeploy(ctx)).To(Succeed())

		for i := range t.expectedSubnetsDeployed {
			t.assertMachineSet(t.machineSets[*t.expectedSubnetsDeployed[i].AvailabilityZone], *t.expectedSubnetsDeployed[i].SubnetId,
				t.expectedInstanceType(), instanceImageID, gatewaySGName)
			delete(t.machineSets, *t.expectedSubnetsDeployed[i].AvailabilityZone)
		}

		Expect(t.machineSets).To(BeEmpty(), "Unexpected machine sets deployed: %#v", t.machineSets)
	})
}

//nolint:gocritic // Error: "consider `machineSets' to be of non-pointer type"
func machineSetFn(machineSets *map[string]*unstructured.Unstructured) func(_ context.Context, ms *unstructured.Unstructured) error {
	*machineSets = map[string]*unstructured.Unstructured{}

	return func(_ context.Context, ms *unstructured.Unstructured) error {
		zone, ok, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value",
			"placement", "availabilityZone")
		Expect(ok).To(BeTrue())

		(*machineSets)[zone] = ms

		return nil
	}
}

func (t *gatewayDeployerTestDriver) assertMachineSet(ms *unstructured.Unstructured, expSubnetID, expInstanceType, expAmiID,
	expGatewaySG string,
) {
	Expect(ms).ToNot(BeNil())

	Expect(ms.GetLabels()).To(HaveKeyWithValue("machine.openshift.io/cluster-api-cluster", infraID))

	instanceType, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "instanceType")
	Expect(instanceType).To(Equal(expInstanceType))

	r, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "placement", "region")
	Expect(r).To(Equal(region))

	amiID, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "ami", "id")
	Expect(amiID).To(Equal(expAmiID))

	securityGroups, _, _ := unstructured.NestedSlice(ms.Object, "spec", "template", "spec", "providerSpec", "value", "securityGroups")
	Expect(securityGroups).To(HaveLen(1))

	sgFilters, _, _ := unstructured.NestedSlice(securityGroups[0].(map[string]any), "filters")
	Expect(sgFilters).To(HaveLen(1))

	filter := sgFilters[0].(map[string]any)
	Expect(filter).To(HaveKeyWithValue("name", "tag:Name"))
	Expect(filter["values"]).To(ContainElements(t.workerSGName))

	if expGatewaySG != "" {
		Expect(filter["values"]).To(ContainElement(expGatewaySG))
	}

	subnetFilters, _, _ := unstructured.NestedSlice(ms.Object, "spec", "template", "spec", "providerSpec", "value", "subnet", "filters")
	Expect(subnetFilters).To(HaveLen(1))

	filter = subnetFilters[0].(map[string]any)
	Expect(filter).To(HaveKeyWithValue("name", "tag:Name"))
	Expect(filter["values"]).To(ContainElement(subnetName(expSubnetID)))
}

func (t *gatewayDeployerTestDriver) setupExpectedSubnetInstanceTypeOfferings(subnets ...types.Subnet) {
	for i := range subnets {
		if t.zonesWithInstanceTypeOfferings.Has(*subnets[i].AvailabilityZone) {
			t.expectDescribeInstanceTypeOfferings(t.expectedInstanceType(), *subnets[i].AvailabilityZone, types.InstanceTypeOffering{})
		} else {
			t.expectDescribeInstanceTypeOfferings(t.expectedInstanceType(), *subnets[i].AvailabilityZone)
		}
	}
}

func (t *gatewayDeployerTestDriver) testCleanupSuccess() {
	It("should delete the correct gateway node machine sets", func(ctx SpecContext) {
		Expect(t.doCleanup(ctx)).To(Succeed())

		for i := range t.existingSubnets {
			subnet := &t.existingSubnets[i]
			t.assertMachineSet(t.machineSets[*subnet.AvailabilityZone], *subnet.SubnetId, t.expectedInstanceType(),
				"", "")
			delete(t.machineSets, *subnet.AvailabilityZone)
		}

		Expect(t.machineSets).To(BeEmpty(), "Unexpected machine sets deleted: %#v", t.machineSets)
	})
}
