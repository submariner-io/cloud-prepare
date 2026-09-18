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
	"context"
	"net/http"
	"slices"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v8"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/stretchr/testify/mock"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/azure"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	ocpFake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
)

const (
	instanceType         = "Standard_D2s_v3"
	imageName            = "test-image"
	nodeName1            = "node1"
	nodeName2            = "node2"
	extSecurityGroupName = testInfraID + azure.ExternalSecurityGroupSuffix
)

var (
	resourceIDImageSpec  = ocp.ImageSpec{ResourceID: imageName}
	marketplaceImageSpec = ocp.ImageSpec{
		Offer:     "aro4",
		Publisher: "azureopenshift",
		SKU:       "420-v2",
		Version:   "9.6.20251015",
		Type:      "MarketplaceNoPlan",
	}
)

var _ = Describe("OCP Gateway Deployer", func() {
	Describe("Deploy", testDeploy)
	Describe("Cleanup", testCleanup)

	Describe("MachineName", func() {
		It("should be at most 20 characters in length", func() {
			Expect(len(azure.MachineName("centralus"))).To(BeNumerically("<=", 20))
			Expect(len(azure.MachineName("verylongregionname"))).To(BeNumerically("<=", 20))
			Expect(azure.MachineName("central")).To(ContainSubstring("central"))
			Expect(azure.MachineName("us")).To(HavePrefix("subgw-us-"))
		})

		It("should generate unique names", func() {
			name1 := azure.MachineName("east")
			name2 := azure.MachineName("east")
			Expect(name1).NotTo(Equal(name2))
		})
	})
})

func testDeploy() { //nolint:maintidx // Deploy test covers many scenarios; splitting would reduce readability.
	t := newGatewayDeployerTestDriver()

	When("gateways are requested", func() {
		BeforeEach(func() {
			t.httpGetResponses[SKUsPath] = &armcompute.ResourceSKUsResult{
				Value: []*armcompute.ResourceSKU{
					{
						Name:         ptr.To(instanceType),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("zone1")},
								Location: ptr.To(testRegion),
							},
						},
					},
					{
						Name:         ptr.To("other-instance-type"),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("other-zone1")},
								Location: ptr.To(testRegion),
							},
						},
					},
					{
						Name:         ptr.To(instanceType),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("zone2")},
								Location: ptr.To(testRegion),
							},
						},
					},
					{
						Name:         ptr.To(instanceType),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("zone3")},
								Location: ptr.To(testRegion),
							},
						},
					},
					{
						Name:         ptr.To(instanceType),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("other-zone2")},
								Location: ptr.To("other-region"),
							},
						},
					},
				},
			}
		})

		It("should deploy gateway machine sets and external port security group rules", func(ctx SpecContext) {
			input := api.GatewayDeployInput{
				Gateways: 2,
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
			Expect(t.deployer.Deploy(ctx, input, reporter.Stdout())).To(Succeed())

			t.assertMachineSets(false, 2, "zone1", "zone2", "zone3")

			var securityGroup armnetwork.SecurityGroup
			t.assertPutRequest(securityGroupPath(extSecurityGroupName), &securityGroup)

			Expect(securityGroup.Properties.SecurityRules).To(ConsistOf(
				append(securityRuleMatchers(input.PublicPorts[0], azure.ExternalSecurityRulePrefix),
					securityRuleMatchers(input.PublicPorts[1], azure.ExternalSecurityRulePrefix)...)))
		})

		Context("and the external security group already exists", func() {
			BeforeEach(func() {
				t.httpGetResponses[securityGroupPath(extSecurityGroupName)] = &armnetwork.SecurityGroup{
					Name: ptr.To(extSecurityGroupName),
				}
			})

			It("should not try to recreate it", func(ctx SpecContext) {
				Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
					Gateways: 1,
				}, reporter.Stdout())).To(Succeed())

				t.assertNoPutRequest(securityGroupPath(extSecurityGroupName))
			})
		})

		Context("with AirGapped specified", func() {
			It("should deploy gateway machine sets without a public IP", func(ctx SpecContext) {
				Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
					Gateways:  1,
					AirGapped: true,
				}, reporter.Stdout())).To(Succeed())

				t.assertMachineSets(true, 1, "zone1", "zone2", "zone3")
			})
		})

		Context("with a Marketplace image (no resourceID)", func() {
			BeforeEach(func() {
				t.workerImage = marketplaceImageSpec
			})

			It("should deploy gateway machine sets using the Marketplace image fields", func(ctx SpecContext) {
				Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
					Gateways: 1,
				}, reporter.Stdout())).To(Succeed())

				t.assertMachineSets(false, 1, "zone1", "zone2", "zone3")
			})
		})
	})

	When("sufficient number of gateways already exist", func() {
		BeforeEach(func() {
			t.existingMachineSets = []unstructured.Unstructured{*t.createMachineSet("existing-gw", "zone1")}
		})

		It("should not deploy new gateways", func(ctx SpecContext) {
			Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
				Gateways: 1,
			}, reporter.Stdout())).To(Succeed())

			Expect(t.machineSetsDeployed).To(BeEmpty())
		})
	})

	When("insufficient number of gateways exist", func() {
		BeforeEach(func() {
			t.existingMachineSets = []unstructured.Unstructured{*t.createMachineSet("existing-gw", "zone1")}

			t.httpGetResponses[SKUsPath] = &armcompute.ResourceSKUsResult{
				Value: []*armcompute.ResourceSKU{
					{
						Name:         ptr.To(instanceType),
						ResourceType: ptr.To(azure.AzureVirtualMachines),
						LocationInfo: []*armcompute.ResourceSKULocationInfo{
							{
								Zones:    []*string{ptr.To("zone1"), ptr.To("zone2")},
								Location: ptr.To(testRegion),
							},
						},
					},
				},
			}
		})

		It("should deploy new gateways to match the requested count", func(ctx SpecContext) {
			Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
				Gateways: 2,
			}, reporter.Stdout())).To(Succeed())

			t.assertMachineSets(false, 1, "zone2")
		})
	})

	When("manually labeled gateway nodes exist", func() {
		BeforeEach(func(ctx SpecContext) {
			for _, name := range []string{nodeName1, nodeName2} {
				t.createGatewayNode(ctx, name)

				nicName := name + "-nic"
				t.httpGetResponses[networkInterfacesPath(nicName)] = &armnetwork.Interface{
					Name: ptr.To(nicName),
					Properties: &armnetwork.InterfacePropertiesFormat{
						IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
							{
								Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
									Primary: ptr.To(true),
								},
							},
						},
					},
				}
			}
		})

		It("should prepare the network interface with a public IP for each node", func(ctx SpecContext) {
			Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
				Gateways: 2,
				PublicPorts: []api.PortSpec{
					{
						Port:     100,
						Protocol: "TCP",
					},
				},
			}, reporter.Stdout())).To(Succeed())

			for _, name := range []string{nodeName1, nodeName2} {
				var publicAddress armnetwork.PublicIPAddress
				t.assertPutRequest(publicAddressesPath(name+"-pub"), &publicAddress)
				Expect(publicAddress.Location).To(Equal(ptr.To(t.cloudInfo.Region)))

				var extSecurityGroup armnetwork.SecurityGroup
				t.assertPutRequest(securityGroupPath(extSecurityGroupName), &extSecurityGroup)

				var netInterface armnetwork.Interface
				t.assertPutRequest(networkInterfacesPath(name+"-nic"), &netInterface)
				Expect(netInterface.Properties.NetworkSecurityGroup).To(Equal(&extSecurityGroup))
				Expect(netInterface.Properties.IPConfigurations).To(HaveLen(1))
				Expect(netInterface.Properties.IPConfigurations[0].Properties.Primary).To(Equal(ptr.To(true)))
				Expect(netInterface.Properties.IPConfigurations[0].Properties.PublicIPAddress).ToNot(BeNil())
			}
		})
	})

	When("no gateways are requested", func() {
		It("should succeed", func(ctx SpecContext) {
			input := api.GatewayDeployInput{
				Gateways: 0,
			}

			err := t.deployer.Deploy(ctx, input, reporter.Stdout())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("no availability zones exist", func() {
		BeforeEach(func() {
			t.httpGetResponses[SKUsPath] = &armcompute.ResourceSKUsResult{}
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
				Gateways: 1,
			}, reporter.Stdout())).NotTo(Succeed())
		})
	})

	When("security group creation fails", func() {
		BeforeEach(func() {
			t.httpPutRespCodes[securityGroupPath(extSecurityGroupName)] = ptr.To(http.StatusUnauthorized)
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(t.deployer.Deploy(ctx, api.GatewayDeployInput{
				Gateways: 1,
			}, reporter.Stdout())).NotTo(Succeed())
		})
	})
}

func testCleanup() {
	t := newGatewayDeployerTestDriver()

	BeforeEach(func(ctx SpecContext) {
		t.existingMachineSets = []unstructured.Unstructured{*t.createMachineSet("existing-ms", "zone1")}

		t.createGatewayNode(ctx, nodeName1)

		publicIPAddress := &armnetwork.PublicIPAddress{
			Name: ptr.To(nodeName1 + "-pub"),
		}

		t.httpGetResponses[publicAddressesPath(nodeName1+"-pub")] = publicIPAddress

		netInterfaceID := "123"

		extSecurityGroup := &armnetwork.SecurityGroup{
			Name: ptr.To(extSecurityGroupName),
			Properties: &armnetwork.SecurityGroupPropertiesFormat{
				NetworkInterfaces: []*armnetwork.Interface{
					{
						ID: ptr.To(netInterfaceID),
					},
				},
			},
		}

		t.httpGetResponses[securityGroupPath(*extSecurityGroup.Name)] = extSecurityGroup

		t.httpGetResponses[networkInterfacesPath("")] = &armnetwork.InterfaceListResult{
			Value: []*armnetwork.Interface{
				{
					Name: ptr.To(nodeName1 + "-nic"),
					ID:   ptr.To(netInterfaceID),
					Properties: &armnetwork.InterfacePropertiesFormat{
						NetworkSecurityGroup: extSecurityGroup,
						IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
							{
								Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
									Primary:         ptr.To(true),
									PublicIPAddress: publicIPAddress,
								},
							},
						},
					},
				},
			},
		}
	})

	It("should delete the gateway machine sets and perform related cleanup", func(ctx SpecContext) {
		Expect(t.deployer.Cleanup(ctx, reporter.Stdout())).To(Succeed())

		Expect(t.existingMachineSets).To(BeEmpty())
		Expect(t.httpGetResponses).NotTo(HaveKey(publicAddressesPath(nodeName1 + "-pub")))
		Expect(t.httpGetResponses).NotTo(HaveKey(securityGroupPath(extSecurityGroupName)))

		node, err := t.kubeClient.CoreV1().Nodes().Get(ctx, nodeName1, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(node.Labels).NotTo(HaveKey(k8s.SubmarinerGatewayLabel))
	})
}

type gatewayDeployerTestDriver struct {
	*testDriver
	msDeployer          *ocpFake.MockMachineSetDeployer
	deployer            api.GatewayDeployer
	existingMachineSets []unstructured.Unstructured
	machineSetsDeployed []*unstructured.Unstructured
	workerImage         ocp.ImageSpec
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{testDriver: newTestDriver()}

	BeforeEach(func() {
		t.existingMachineSets = nil
		t.machineSetsDeployed = nil
		t.workerImage = resourceIDImageSpec
		t.msDeployer = ocpFake.NewMockMachineSetDeployer(GinkgoT())
	})

	JustBeforeEach(func() {
		t.deployer = azure.NewOcpGatewayDeployer(&t.cloudInfo, t.msDeployer, instanceType)

		t.msDeployer.EXPECT().GetWorkerNodeImage(mock.Anything, mock.Anything, t.cloudInfo.InfraID).Return(t.workerImage, nil).Maybe()
		t.msDeployer.EXPECT().List(mock.Anything).Return(slices.Clone(t.existingMachineSets), nil).Maybe()
		t.msDeployer.EXPECT().Deploy(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, ms *unstructured.Unstructured) error {
				t.machineSetsDeployed = append(t.machineSetsDeployed, ms)
				return nil
			}).Maybe()

		t.msDeployer.EXPECT().DeleteByName(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, msName, _ string) error {
				t.existingMachineSets = slices.DeleteFunc(t.existingMachineSets, func(u unstructured.Unstructured) bool {
					name, _, _ := unstructured.NestedString(u.Object, "metadata", "name")
					return name == msName
				})

				return nil
			}).Maybe()
	})

	return t
}

func (t *gatewayDeployerTestDriver) assertMachineSets(airGapped bool, count int, zones ...string) {
	t.assertMachineSetsWithImage(airGapped, &t.workerImage, count, zones...)
}

func (t *gatewayDeployerTestDriver) assertMachineSetsWithImage(airGapped bool, image *ocp.ImageSpec, count int, zones ...string) {
	zoneMatchers := make([]types.GomegaMatcher, len(zones))
	for i := range zones {
		zoneMatchers[i] = Equal(zones[i])
	}

	for _, ms := range t.machineSetsDeployed {
		Expect(ms.GetLabels()).To(HaveKeyWithValue("machine.openshift.io/cluster-api-cluster", t.cloudInfo.InfraID))

		values, found, err := unstructured.NestedMap(ms.Object, "spec", "template", "spec", "providerSpec", "value")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(values).To(HaveKeyWithValue("apiVersion", "azureproviderconfig.openshift.io/v1beta1"))
		Expect(values).To(HaveKeyWithValue("kind", "AzureMachineProviderSpec"))
		Expect(values).To(HaveKeyWithValue("vmSize", instanceType))
		Expect(values).To(HaveKeyWithValue("location", t.cloudInfo.Region))
		Expect(values).To(HaveKeyWithValue("securityGroup", extSecurityGroupName))
		Expect(values).To(HaveKeyWithValue("publicIP", !airGapped))

		if image.ResourceID != "" {
			actual, _, _ := unstructured.NestedString(values, "image", "resourceID")
			Expect(actual).To(Equal(image.ResourceID))
		} else {
			actual, _, _ := unstructured.NestedString(values, "image", "offer")
			Expect(actual).To(Equal(image.Offer))
			actual, _, _ = unstructured.NestedString(values, "image", "publisher")
			Expect(actual).To(Equal(image.Publisher))
			actual, _, _ = unstructured.NestedString(values, "image", "sku")
			Expect(actual).To(Equal(image.SKU))
			actual, _, _ = unstructured.NestedString(values, "image", "version")
			Expect(actual).To(Equal(image.Version))
			actual, _, _ = unstructured.NestedString(values, "image", "type")
			Expect(actual).To(Equal(image.Type))
		}

		Expect(values).To(HaveKeyWithValue("zone", Or(zoneMatchers...)))
	}

	Expect(t.machineSetsDeployed).To(HaveLen(count))
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

func (t *gatewayDeployerTestDriver) createMachineSet(name, zone string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "machine.openshift.io/v1beta1",
			"kind":       "MachineSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "openshift-machine-api",
				"labels": map[string]any{
					"machine.openshift.io/cluster-api-cluster": t.cloudInfo.InfraID,
				},
			},
			"spec": map[string]any{
				"template": map[string]any{
					"spec": map[string]any{
						"providerSpec": map[string]any{
							"value": map[string]any{
								"zone": zone,
							},
						},
					},
				},
			},
		},
	}
}
