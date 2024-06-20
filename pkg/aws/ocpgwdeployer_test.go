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

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/aws"
	ocpFake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/set"
)

var _ = Describe("OCP GatewayDeployer", func() {
	Context("on Deploy", testDeploy)
	Context("on Cleanup", testCleanup)
})

func testDeploy() {
	t := newGatewayDeployerTestDriver()

	var deployCall *gomock.Call

	JustBeforeEach(func() {
		deployCall = t.msDeployer.EXPECT().Deploy(gomock.Any()).DoAndReturn(machineSetFn(&t.machineSets))

		t.expectDescribePublicSubnets(t.subnets...)

		for i := range t.subnets {
			if t.zonesWithInstanceTypeOfferings.Has(*t.subnets[i].AvailabilityZone) {
				t.expectDescribeInstanceTypeOfferings(t.expectedInstanceType(), *t.subnets[i].AvailabilityZone, types.InstanceTypeOffering{})
			} else {
				t.expectDescribeInstanceTypeOfferings(t.expectedInstanceType(), *t.subnets[i].AvailabilityZone)
			}
		}
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

			t.doDeploy()
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
				t.zonesWithInstanceTypeOfferings = set.New(availabilityZone2)
				t.expectedSubnetsDeployed = []types.Subnet{t.subnets[1]}
				t.expectedSubnetsTagged = t.expectedSubnetsDeployed
			})

			t.testDeploySuccess("", "")
		})

		Context("and the deploying subnet is already tagged", func() {
			BeforeEach(func() {
				t.expectedSubnetsTagged = nil
				t.subnets[0].Tags = append(t.subnets[0].Tags, types.Tag{
					Key:   awssdk.String("submariner.io/gateway"),
					Value: awssdk.String(""),
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
	})

	Context("", func() {
		JustBeforeEach(func() {
			deployCall.AnyTimes()
			t.doDeploy()
		})

		BeforeEach(func() {
			t.expectDeployValidations(false)
		})

		When("the infra ID VPC does not exist", func() {
			BeforeEach(func() {
				t.vpcID = ""
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})

		When("the retrieval of public subnets fails", func() {
			BeforeEach(func() {
				t.describeSubnetsErr = errors.New("mock error")
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})

		When("tagging a public subnet fails", func() {
			BeforeEach(func() {
				t.createTagsErr = errors.New("mock error")
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(100, "TCP"))
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(200, "UDP"))
				t.expectCreateGatewayTags(*t.expectedSubnetsTagged[0].SubnetId)
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})

		When("the creation of a security group fails", func() {
			BeforeEach(func() {
				t.authorizeSecurityGroupIngressErr = errors.New("mock error")
				t.expectAuthorizeSecurityGroupIngress(gatewayGroupID, newPublicSGRule(100, "TCP"))
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})

		When("there's an insufficient number of public subnets", func() {
			BeforeEach(func() {
				t.subnets = nil
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})
	})
}

func testCleanup() {
	t := newGatewayDeployerTestDriver()

	JustBeforeEach(func() {
		t.expectDescribeGatewaySubnets(t.subnets...)
		t.doCleanup()
	})

	When("on success", func() {
		BeforeEach(func() {
			t.expectCleanupValidations(true)
			t.msDeployer.EXPECT().Delete(gomock.Any()).DoAndReturn(machineSetFn(&t.machineSets)).Times(len(t.subnets))
			t.expectDeleteSecurityGroup(gatewayGroupID)

			for i := range t.subnets {
				t.expectDeleteGatewayTags(*t.subnets[i].SubnetId)
			}
		})

		It("should delete the correct gateway node machine sets", func() {
			Expect(t.retError).To(Succeed())

			for i := range t.subnets {
				assertMachineSet(t.machineSets[*t.subnets[i].AvailabilityZone], *t.subnets[i].SubnetId, t.expectedInstanceType(),
					"", "")
				delete(t.machineSets, *t.subnets[i].AvailabilityZone)
			}

			Expect(t.machineSets).To(HaveLen(0), "Unexpected machine sets deleted: %#v", t.machineSets)
		})
	})

	Context("", func() {
		BeforeEach(func() {
			t.expectCleanupValidations(false)
		})

		When("the infra ID VPC does not exist", func() {
			BeforeEach(func() {
				t.vpcID = ""
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})

		When("the retrieval of public subnets fails", func() {
			BeforeEach(func() {
				t.describeSubnetsErr = errors.New("mock error")
			})

			It("should return an error", func() {
				Expect(t.retError).To(HaveOccurred())
			})
		})
	})
}

type gatewayDeployerTestDriver struct {
	fakeAWSClientBase
	numGateways                    int
	instanceType                   string
	subnets                        []types.Subnet
	expectedSubnetsDeployed        []types.Subnet
	expectedSubnetsTagged          []types.Subnet
	gatewayGroupID                 string
	zonesWithInstanceTypeOfferings set.Set[string]
	machineSets                    map[string]*unstructured.Unstructured
	retError                       error
	msDeployer                     *ocpFake.MockMachineSetDeployer
	gwDeployer                     api.GatewayDeployer
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{}

	BeforeEach(func() {
		t.beforeEach()

		t.msDeployer = ocpFake.NewMockMachineSetDeployer(t.mockCtrl)
		t.numGateways = 1
		t.instanceType = "test-instance-type"
		t.subnets = []types.Subnet{newSubnet(availabilityZone1, subnetID1), newSubnet(availabilityZone2, subnetID2)}
		t.expectedSubnetsDeployed = []types.Subnet{t.subnets[0]}
		t.expectedSubnetsTagged = []types.Subnet{t.subnets[0]}
		t.gatewayGroupID = gatewayGroupID
		t.zonesWithInstanceTypeOfferings = set.New[string]()

		for i := range t.subnets {
			t.zonesWithInstanceTypeOfferings.Insert(*t.subnets[i].AvailabilityZone)
		}
	})

	JustBeforeEach(func() {
		t.expectDescribeVpcs(t.vpcID)
		t.expectDescribeSecurityGroups(gatewaySGName, t.gatewayGroupID)
		t.expectDescribeInstances(instanceImageID)
		t.expectDescribeSecurityGroups(workerSGName, workerGroupID)
		t.expectDescribePublicSubnets(t.subnets...)
		t.expectDescribeVpcsSigs(t.vpcID)
		t.expectDescribePublicSubnetsSigs(t.subnets...)

		var err error

		t.gwDeployer, err = aws.NewOcpGatewayDeployer(aws.NewCloud(t.awsClient, infraID, region), t.msDeployer, t.instanceType)
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

func (t *gatewayDeployerTestDriver) doDeploy() {
	t.retError = t.gwDeployer.Deploy(api.GatewayDeployInput{
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

func (t *gatewayDeployerTestDriver) doCleanup() {
	t.retError = t.gwDeployer.Cleanup(reporter.Stdout())
}

func (t *gatewayDeployerTestDriver) expectDeployValidations(enforce bool) {
	calls := []*gomock.Call{
		t.expectValidateCreateSecurityGroup(),
		t.expectValidateAuthorizeSecurityGroupIngress(nil),
		t.expectValidateDescribeInstanceTypeOfferings(),
		t.expectValidateCreateTags(),
	}

	for _, c := range calls {
		if !enforce {
			c.AnyTimes()
		}
	}
}

func (t *gatewayDeployerTestDriver) expectCleanupValidations(enforce bool) {
	calls := []*gomock.Call{
		t.expectValidateDeleteSecurityGroup(),
		t.expectValidateDeleteTags(),
	}

	for _, c := range calls {
		if !enforce {
			c.AnyTimes()
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

	It(msg+" deploy the correct gateway node machine sets"+msgSuffix, func() {
		Expect(t.retError).To(Succeed())

		for i := range t.expectedSubnetsDeployed {
			assertMachineSet(t.machineSets[*t.expectedSubnetsDeployed[i].AvailabilityZone], *t.expectedSubnetsDeployed[i].SubnetId,
				t.expectedInstanceType(), instanceImageID, gatewaySGName)
			delete(t.machineSets, *t.expectedSubnetsDeployed[i].AvailabilityZone)
		}

		Expect(t.machineSets).To(HaveLen(0), "Unexpected machine sets deployed: %#v", t.machineSets)
	})
}

//nolint:gocritic // Error: "consider `machineSets' to be of non-pointer type"
func machineSetFn(machineSets *map[string]*unstructured.Unstructured) func(ms *unstructured.Unstructured) error {
	*machineSets = map[string]*unstructured.Unstructured{}

	return func(ms *unstructured.Unstructured) error {
		zone, ok, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value",
			"placement", "availabilityZone")
		Expect(ok).To(BeTrue())

		(*machineSets)[zone] = ms

		return nil
	}
}

func assertMachineSet(ms *unstructured.Unstructured, expSubnetID, expInstanceType, expAmiID, expGatewaySG string) {
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

	sgFilters, _, _ := unstructured.NestedSlice(securityGroups[0].(map[string]interface{}), "filters")
	Expect(sgFilters).To(HaveLen(1))

	filter := sgFilters[0].(map[string]interface{})
	Expect(filter).To(HaveKeyWithValue("name", "tag:Name"))
	Expect(filter["values"]).To(ContainElements(workerSGName))

	if expGatewaySG != "" {
		Expect(filter["values"]).To(ContainElement(expGatewaySG))
	}

	subnetFilters, _, _ := unstructured.NestedSlice(ms.Object, "spec", "template", "spec", "providerSpec", "value", "subnet", "filters")
	Expect(subnetFilters).To(HaveLen(1))

	filter = subnetFilters[0].(map[string]interface{})
	Expect(filter).To(HaveKeyWithValue("name", "tag:Name"))
	Expect(filter["values"]).To(ContainElement(subnetName(expSubnetID)))
}
