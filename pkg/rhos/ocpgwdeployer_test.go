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
	"slices"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	ocpfake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	"github.com/submariner-io/cloud-prepare/pkg/rhos"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("OCP Gateway Deployer", func() {
	Context("Deploy", testDeploy)
	Context("Cleanup", testCleanup)
})

func testDeploy() {
	t := newGatewayDeployerTestDriver()

	BeforeEach(func() {
		t.msDeployer.EXPECT().GetWorkerNodeImage(mock.Anything, mock.Anything, testInfraID).Return(ocp.ImageSpec{Image: testImage}, nil).Maybe()
	})

	It("should deploy a gateway node machine set and security group rules", func(ctx SpecContext) {
		Expect(t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout())).To(Succeed())

		t.assertMachineSet(false, SubnetParam{
			Filter: SubnetFilter{
				Name: testInfraID + "-nodes",
				Tags: "openshiftClusterID=" + testInfraID,
			},
		})

		Expect(t.securityGroupsCreated).To(ContainElement(ContainSubstring(rhos.GwSecurityGroupSuffix)))

		t.assertRuleCreated(rhos.GwSecurityGroupSuffix, t.gwDeployInput.PublicPorts[0])
		t.assertRuleCreated(rhos.GwSecurityGroupSuffix, t.gwDeployInput.PublicPorts[1])
	})

	When("custom subnets are provided", func() {
		const (
			subnetName1 = "subnet1"
			subnetName2 = "subnet2"
		)

		BeforeEach(func() {
			t.subnetNames = []string{subnetName1, subnetName2}
		})

		It("should deploy a gateway node machine with the subnets applied", func(ctx SpecContext) {
			Expect(t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout())).To(Succeed())

			t.assertMachineSet(false, SubnetParam{
				Filter: SubnetFilter{
					Name: subnetName1,
				},
			}, SubnetParam{
				Filter: SubnetFilter{
					Name: subnetName2,
				},
			})
		})
	})

	When("the internal security group exists", func() {
		BeforeEach(func() {
			t.existingSecurityGroups = []secgroups.SecurityGroup{
				{
					Name: internalSecurityGroup,
				},
			}
		})

		It("should deploy a gateway node machine set with the internal security group", func(ctx SpecContext) {
			Expect(t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout())).To(Succeed())
			t.assertMachineSet(true)
		})
	})

	When("the gateway security group already exists", func() {
		BeforeEach(func() {
			t.existingSecurityGroups = []secgroups.SecurityGroup{
				{
					Name: gwSecurityGroup,
				},
			}
		})

		It("should not try to recreate it", func(ctx SpecContext) {
			Expect(t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout())).To(Succeed())
			Expect(t.securityGroupsCreated).To(BeEmpty())
		})
	})

	When("a node is already labeled as a gateway", func() {
		BeforeEach(func(ctx SpecContext) {
			for _, name := range []string{nodeName1, nodeName2} {
				t.createGatewayNode(ctx, name)
			}
		})

		It("should open the gateway port and not deploy a machine set", func(ctx SpecContext) {
			Expect(t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout())).To(Succeed())
			t.assertServerSecGroup(gwSecurityGroup)
		})

		t.testErrors(func(ctx context.Context) error { return t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout()) },
			extractServersErrEntry())
	})

	t.testErrors(func(ctx context.Context) error { return t.gwDeployer.Deploy(ctx, t.gwDeployInput, reporter.Stdout()) },
		newComputeV2ErrEntry(),
		newNetworkV2ErrEntry(),
		createSecurityGroupErrEntry(),
		extractSecurityGroupsErrEntry())
}

func testCleanup() {
	t := newGatewayDeployerTestDriver()

	BeforeEach(func(ctx SpecContext) {
		t.existingMachineSets = []unstructured.Unstructured{
			{
				Object: map[string]any{
					"apiVersion": "machine.openshift.io/v1beta1",
					"kind":       "MachineSet",
					"metadata": map[string]any{
						"name": nodeName1,
					},
				},
			},
			{
				Object: map[string]any{
					"apiVersion": "machine.openshift.io/v1beta1",
					"kind":       "MachineSet",
					"metadata": map[string]any{
						"name": nodeName2,
					},
				},
			},
		}

		t.existingSecurityGroups = []secgroups.SecurityGroup{
			{
				Name: gwSecurityGroup,
				ID:   gwSecurityGroup,
			},
		}

		t.servers[nodeName3] = &servers.Server{ID: serverID3}

		t.createGatewayNode(ctx, nodeName3)
	})

	It("should delete the gateway machine sets and security groups", func(ctx SpecContext) {
		Expect(t.gwDeployer.Cleanup(ctx, reporter.Stdout())).To(Succeed())
		Expect(t.existingMachineSets).To(BeEmpty())
		Expect(t.existingSecurityGroups).To(BeEmpty())
		t.assertNoServerSecGroup(gwSecurityGroup)

		node, err := t.kubeClient.CoreV1().Nodes().Get(ctx, nodeName3, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(node.Labels).NotTo(HaveKey(k8s.SubmarinerGatewayLabel))
	})

	t.testErrors(func(ctx context.Context) error { return t.gwDeployer.Cleanup(ctx, reporter.Stdout()) },
		newComputeV2ErrEntry(),
		deleteSecurityGroupErrEntry(),
		extractSecurityGroupsErrEntry(),
		extractServersErrEntry(),
		removeServerErrEntry())
}

type gatewayDeployerTestDriver struct {
	*testDriver
	msDeployer          *ocpfake.MockMachineSetDeployer
	gwDeployer          api.GatewayDeployer
	kubeClient          *k8sfake.Clientset
	machineSetsDeployed []*unstructured.Unstructured
	existingMachineSets []unstructured.Unstructured
	gwDeployInput       api.GatewayDeployInput
	subnetNames         []string
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{testDriver: newTestDriver()}

	BeforeEach(func() {
		t.msDeployer = ocpfake.NewMockMachineSetDeployer(GinkgoT())
		t.kubeClient = k8sfake.NewClientset()
		t.machineSetsDeployed = nil
		t.existingMachineSets = nil
		t.subnetNames = nil

		t.gwDeployInput = api.GatewayDeployInput{
			Gateways: 1,
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
		}
	})

	JustBeforeEach(func() {
		t.msDeployer.EXPECT().Deploy(mock.Anything, mock.Anything).RunAndReturn(t.machineSetFn()).Maybe()
		t.msDeployer.EXPECT().List(mock.Anything).Return(slices.Clone(t.existingMachineSets), nil).Maybe()

		t.msDeployer.EXPECT().DeleteByName(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, msName, _ string) error {
				t.existingMachineSets = slices.DeleteFunc(t.existingMachineSets, func(u unstructured.Unstructured) bool {
					name, _, _ := unstructured.NestedString(u.Object, "metadata", "name")
					return name == msName
				})

				return nil
			}).Maybe()

		t.gwDeployer = rhos.NewOcpGatewayDeployer(rhos.CloudInfo{
			Client:      &gophercloud.ProviderClient{},
			InfraID:     testInfraID,
			Region:      testRegion,
			K8sClient:   k8s.NewInterface(t.kubeClient),
			SubnetNames: t.subnetNames,
		}, t.msDeployer, testProjectID, testInstanceType, "", testCloudName)
	})

	return t
}

func (t *gatewayDeployerTestDriver) machineSetFn() func(_ context.Context, ms *unstructured.Unstructured) error {
	return func(_ context.Context, ms *unstructured.Unstructured) error {
		t.machineSetsDeployed = append(t.machineSetsDeployed, ms)
		return nil
	}
}

func (t *gatewayDeployerTestDriver) assertMachineSet(useInternalSG bool, expSubnets ...SubnetParam) {
	Expect(t.machineSetsDeployed).To(HaveLen(1))

	ms := t.machineSetsDeployed[0]
	Expect(ms.GetLabels()).To(HaveKeyWithValue("machine.openshift.io/cluster-api-cluster", testInfraID))
	values, found, err := unstructured.NestedMap(ms.Object, "spec", "template", "spec", "providerSpec", "value")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())

	Expect(values).To(HaveKeyWithValue("cloudName", testCloudName))
	Expect(values).To(HaveKeyWithValue("flavor", testInstanceType))
	Expect(values).To(HaveKeyWithValue("image", testImage))

	tags, found, err := unstructured.NestedSlice(values, "tags")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	Expect(tags).To(ContainElement(rhos.SubmarinerGatewayNodeTag))

	networks, found, err := unstructured.NestedSlice(values, "networks")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	Expect(networks).To(HaveLen(1))

	if len(expSubnets) > 0 {
		subnetObjs, found, err := unstructured.NestedSlice(networks[0].(map[string]any), "subnets")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		actualSubnets := make([]SubnetParam, 0, len(subnetObjs))

		for _, s := range subnetObjs {
			var subnet SubnetParam
			Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(s.(map[string]any), &subnet)).To(Succeed())

			actualSubnets = append(actualSubnets, subnet)
		}

		Expect(actualSubnets).To(ConsistOf(expSubnets))
	}

	secGroups, found, err := unstructured.NestedSlice(values, "securityGroups")
	Expect(err).NotTo(HaveOccurred())
	Expect(found).To(BeTrue())
	Expect(slices.ContainsFunc(secGroups, func(i any) bool {
		return i.(map[string]any)["name"] == testInfraID+rhos.InternalSecurityGroupSuffix
	})).To(Equal(useInternalSG))
}

func (t *gatewayDeployerTestDriver) createGatewayNode(ctx context.Context, name string) {
	_, err := t.kubeClient.CoreV1().Nodes().Create(ctx, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{k8s.SubmarinerGatewayLabel: "true"},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}
