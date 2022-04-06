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

package gcp_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/gcp"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	ocpFake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeFake "k8s.io/client-go/kubernetes/fake"
)

const (
	publicPortsRuleName      = "test-infraID-submariner-public-ports-ingress"
	submarinerGatewayNodeTag = "submariner-io-gateway-node"
)

var _ = Describe("OCP GatewayDeployer", func() {
	Context("on Deploy", testDeploy)
	Context("on Cleanup", testCleanup)
})

func testDeploy() {
	t := newGatewayDeployerTestDriver()

	var (
		actualRule *compute.Firewall
		retError   error
	)

	BeforeEach(func() {
		actualRule = nil

		t.gcpClient.EXPECT().GetFirewallRule(projectID, publicPortsRuleName).Return(nil, &googleapi.Error{Code: http.StatusNotFound})
		t.gcpClient.EXPECT().InsertFirewallRule(projectID, gomock.Any()).DoAndReturn(func(_ string, rule *compute.Firewall) error {
			actualRule = rule
			return nil
		})
	})

	JustBeforeEach(func() {
		retError = t.doDeploy()
	})

	It("should insert the firewall rule", func() {
		Expect(actualRule).ToNot(BeNil(), "InsertFirewallRule was not called")
		t.assertIngressRule(actualRule)
	})

	When("one gateway is requested", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNode("node-1", zone1, instance1),
				newNode("node-2", "other", "other"),
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-3",
					},
				},
			}

			t.expInstanceTagged(zone1, t.instances[zone1][0])
			t.numGateways = 1
		})

		It("should label one gateway node", func() {
			Expect(retError).To(Succeed())
			t.assertLabeledNodes("node-1")
		})
	})

	When("two gateways are requested", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNode("node-1", zone1, instance1),
			}

			t.expInstanceTagged(zone1, t.instances[zone1][0])
			t.numGateways = 2
		})

		Context("", func() {
			BeforeEach(func() {
				t.nodes = append(t.nodes, newNode("node-2", zone2, instance2))
				t.expInstanceTagged(zone2, t.instances[zone2][0])
			})

			It("should label two gateway nodes", func() {
				Expect(retError).To(Succeed())
				t.assertLabeledNodes("node-1", "node-2")
			})
		})

		Context("and there's an insufficient number of available nodes", func() {
			It("should partially label the gateways", func() {
				Expect(retError).ToNot(Succeed())
				t.assertLabeledNodes("node-1")
			})
		})
	})

	When("the requested number of gateways is increased", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNode("node-1", zone1, instance1),
				newNode("node-2", zone2, instance2),
			}

			t.instances[zone1][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.expInstanceTagged(zone2, t.instances[zone2][0])
			t.numGateways = 2
		})

		It("should label the additional gateway nodes", func() {
			Expect(retError).To(Succeed())
			t.assertLabeledNodes("node-2")
		})
	})

	When("the requested number of gateways is decreased", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				labelNode(newNode("node-1", zone1, instance1)),
				labelNode(newNode("node-2", zone2, instance2)),
			}

			t.instances[zone1][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.instances[zone2][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.numGateways = 1
		})

		It("should do nothing", func() {
			Expect(retError).To(Succeed())
			t.assertLabeledNodes("node-1", "node-2")
		})
	})

	When("the requested number of gateway nodes are already labeled", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				labelNode(newNode("node-1", zone1, instance1)),
				labelNode(newNode("node-2", zone2, instance2)),
			}

			t.instances[zone1][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.instances[zone2][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.numGateways = 2
		})

		It("should not try to update them", func() {
			Expect(retError).To(Succeed())

			actualActions := t.kubeClient.Fake.Actions()
			for i := range actualActions {
				if actualActions[i].GetResource().Resource == "nodes" {
					Expect(actualActions[i].GetVerb()).ToNot(Equal("update"))
				}
			}
		})
	})

	When("dedicated gateway nodes are requested", func() {
		var machineSets map[string]*unstructured.Unstructured

		BeforeEach(func() {
			t.msDeployer.EXPECT().GetWorkerNodeImage(gomock.Any(), infraID).Return("test-image", nil).AnyTimes()
			t.msDeployer.EXPECT().Deploy(gomock.Any()).DoAndReturn(machineSetFn(&machineSets)).Times(2)

			t.dedicatedGWNode = true
			t.numGateways = 2
		})

		It("should deploy the desired number of gateway nodes", func() {
			Expect(retError).To(Succeed())

			Expect(machineSets).To(HaveLen(2))
			t.assertMachineSet(machineSets[zone1], "test-image")
			t.assertMachineSet(machineSets[zone2], "test-image")
		})

		Context("with a specific image", func() {
			BeforeEach(func() {
				t.image = "custom-image"
			})

			It("should deploy the gateway nodes with that image", func() {
				Expect(retError).To(Succeed())

				Expect(machineSets).To(HaveLen(2))
				t.assertMachineSet(machineSets[zone1], "custom-image")
				t.assertMachineSet(machineSets[zone2], "custom-image")
			})
		})
	})

	When("zone retrieval fails", func() {
		BeforeEach(func() {
			t.gcpClient.EXPECT().ListZones().Return(nil, errors.New("fake error"))
		})

		It("should return an error", func() {
			Expect(retError).ToNot(Succeed())
		})
	})
}

func testCleanup() {
	t := newGatewayDeployerTestDriver()

	var (
		retError           error
		deleteFirewallRule error
	)

	BeforeEach(func() {
		deleteFirewallRule = nil
	})

	JustBeforeEach(func() {
		t.gcpClient.EXPECT().DeleteFirewallRule(projectID, publicPortsRuleName).Return(deleteFirewallRule)
		retError = t.gwDeployer.Cleanup(reporter.Stdout())
	})

	It("should delete the firewall rule", func() {
		Expect(retError).To(Succeed())
	})

	Context("with preexisting nodes labeled as gateways", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				labelNode(newNode("node-1", zone1, instance1)),
				labelNode(newNode("node-2", zone2, instance2)),
			}

			t.instances[zone1][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.instances[zone2][0].Tags.Items = []string{submarinerGatewayNodeTag}

			t.expInstanceUntagged(zone1, t.instances[zone1][0])
			t.expInstanceUntagged(zone2, t.instances[zone2][0])
		})

		It("should unlabel them", func() {
			Expect(retError).To(Succeed())
			t.assertLabeledNodes()
		})
	})

	Context("with dedicated nodes deployed as gateways", func() {
		var machineSets map[string]*unstructured.Unstructured

		BeforeEach(func() {
			t.instances[zone1][0].Name = infraID + "-submariner-gw-" + zone1
			t.instances[zone2][0].Name = infraID + "-submariner-gw-" + zone2

			t.instances[zone1][0].Tags.Items = []string{submarinerGatewayNodeTag}
			t.instances[zone2][0].Tags.Items = []string{submarinerGatewayNodeTag}

			t.msDeployer.EXPECT().Delete(gomock.Any()).DoAndReturn(machineSetFn(&machineSets)).Times(2)
		})

		It("should delete them", func() {
			Expect(retError).To(Succeed())

			Expect(machineSets).To(HaveLen(2))
			t.assertMachineSet(machineSets[zone1], "")
			t.assertMachineSet(machineSets[zone2], "")
		})
	})

	When("zone retrieval fails", func() {
		BeforeEach(func() {
			t.gcpClient.EXPECT().ListZones().Return(nil, errors.New("fake error"))
		})

		It("should return an error", func() {
			Expect(retError).ToNot(Succeed())
		})
	})

	When("firewall rule deletion fails", func() {
		BeforeEach(func() {
			deleteFirewallRule = errors.New("fake error")
		})

		It("should return an error", func() {
			Expect(retError).ToNot(Succeed())
		})
	})
}

type gatewayDeployerTestDriver struct {
	fakeGCPClientBase
	numGateways     int
	dedicatedGWNode bool
	image           string
	kubeClient      *kubeFake.Clientset
	msDeployer      *ocpFake.MockMachineSetDeployer
	nodes           []*corev1.Node
	zones           []*compute.Zone
	instances       map[string][]*compute.Instance
	gwDeployer      api.GatewayDeployer
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{}

	BeforeEach(func() {
		t.beforeEach()

		t.nodes = []*corev1.Node{}

		t.zones = []*compute.Zone{
			{
				Name:   zone1,
				Region: "east/" + region,
			},
			{
				Name:   zone2,
				Region: "west/" + region,
			},
			{
				Name:   "other-zone",
				Region: "other-region",
			},
		}

		t.instances = map[string][]*compute.Instance{
			zone1: {
				{
					Name: instance1,
					Tags: &compute.Tags{},
				},
				{
					Name: "other-instance",
				},
			},
			zone2: {
				{
					Name: instance2,
					Tags: &compute.Tags{},
				},
			},
		}

		t.dedicatedGWNode = false
		t.image = ""
		t.msDeployer = ocpFake.NewMockMachineSetDeployer(t.mockCtrl)
		t.kubeClient = kubeFake.NewSimpleClientset()
	})

	JustBeforeEach(func() {
		t.gcpClient.EXPECT().ListZones().Return(&compute.ZoneList{Items: t.zones}, nil).AnyTimes()
		t.gcpClient.EXPECT().ListInstances(gomock.Any()).DoAndReturn(func(zone string) (*compute.InstanceList, error) {
			list := t.instances[zone]
			if list != nil {
				return &compute.InstanceList{Items: list}, nil
			}

			return &compute.InstanceList{}, nil
		}).AnyTimes()

		t.gcpClient.EXPECT().GetInstance(gomock.Any(), gomock.Any()).DoAndReturn(func(zone, instance string) (*compute.Instance, error) {
			list := t.instances[zone]
			for _, i := range list {
				if i.Name == instance {
					return i, nil
				}
			}

			return nil, fmt.Errorf("instance %q not found", instance)
		}).AnyTimes()

		for _, node := range t.nodes {
			_, err := t.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
			Expect(err).To(Succeed())
		}

		t.kubeClient.ClearActions()

		t.gwDeployer = gcp.NewOcpGatewayDeployer(gcp.CloudInfo{
			InfraID:   infraID,
			Region:    region,
			ProjectID: projectID,
			Client:    t.gcpClient,
		}, t.msDeployer, instanceType, t.image, t.dedicatedGWNode, k8s.NewInterface(t.kubeClient))
	})

	return t
}

func (t *gatewayDeployerTestDriver) doDeploy() error {
	return t.gwDeployer.Deploy(api.GatewayDeployInput{
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

func (t *gatewayDeployerTestDriver) getLabeledNodes() []string {
	found := []string{}

	for _, expected := range t.nodes {
		actual, err := t.kubeClient.CoreV1().Nodes().Get(context.TODO(), expected.Name, metav1.GetOptions{})
		Expect(err).To(Succeed())

		if actual.Labels["submariner.io/gateway"] == "true" {
			found = append(found, actual.Name)
		}
	}

	return found
}

func (t *gatewayDeployerTestDriver) assertLabeledNodes(expNodes ...string) {
	actual := t.getLabeledNodes()
	Expect(actual).To(HaveLen(len(expNodes)))

	for _, n := range expNodes {
		Expect(actual).To(ContainElement(n))
	}
}

func (t *gatewayDeployerTestDriver) assertIngressRule(rule *compute.Firewall) {
	Expect(rule.Name).To(Equal(publicPortsRuleName))
	Expect(rule.Direction).To(Equal("INGRESS"))
	Expect(rule.Allowed).To(HaveLen(2))
	Expect(rule.Allowed[0]).To(Equal(&compute.FirewallAllowed{
		IPProtocol: "TCP",
		Ports:      []string{"100"},
	}))
	Expect(rule.Allowed[1]).To(Equal(&compute.FirewallAllowed{
		IPProtocol: "UDP",
		Ports:      []string{"200"},
	}))
}

func (t *gatewayDeployerTestDriver) expInstanceTagged(zone string, instance *compute.Instance) {
	t.gcpClient.EXPECT().UpdateInstanceNetworkTags(projectID, zone, instance.Name, &compute.Tags{
		Items: []string{submarinerGatewayNodeTag},
	})

	t.gcpClient.EXPECT().ConfigurePublicIPOnInstance(instance)
}

func (t *gatewayDeployerTestDriver) expInstanceUntagged(zone string, instance *compute.Instance) {
	t.gcpClient.EXPECT().UpdateInstanceNetworkTags(projectID, zone, instance.Name, &compute.Tags{
		Items: []string{},
	})

	t.gcpClient.EXPECT().DeletePublicIPOnInstance(instance)
}

func (t *gatewayDeployerTestDriver) assertMachineSet(ms *unstructured.Unstructured, expImage string) {
	Expect(ms).ToNot(BeNil())

	Expect(ms.GetLabels()).To(HaveKeyWithValue("machine.openshift.io/cluster-api-cluster", infraID))

	zone, ok, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "zone")
	Expect(ok).To(BeTrue())
	Expect(ms.GetName()).To(Equal(infraID + "-submariner-gw-" + zone))

	mt, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "machineType")
	Expect(mt).To(Equal(instanceType))

	projectID, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "projectID")
	Expect(projectID).To(Equal(projectID))

	r, _, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "region")
	Expect(r).To(Equal(region))

	disks, ok, _ := unstructured.NestedSlice(ms.Object, "spec", "template", "spec", "providerSpec", "value", "disks")
	Expect(ok).To(BeTrue())
	Expect(disks).To(HaveLen(1))
	image, _, _ := unstructured.NestedString(disks[0].(map[string]interface{}), "image")
	Expect(image).To(Equal(expImage))
}

func newNode(name, withZone, withInstance string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"topology.kubernetes.io/zone":    withZone,
				"node-role.kubernetes.io/worker": "",
			},
			Annotations: map[string]string{
				"machine.openshift.io/machine": "east/" + withInstance,
			},
		},
	}
}

func labelNode(node *corev1.Node) *corev1.Node {
	node.Labels["submariner.io/gateway"] = "true"
	return node
}

// nolint:gocritic // Error: "consider `machineSets' to be of non-pointer type"
func machineSetFn(machineSets *map[string]*unstructured.Unstructured) func(ms *unstructured.Unstructured) error {
	*machineSets = map[string]*unstructured.Unstructured{}

	return func(ms *unstructured.Unstructured) error {
		zone, ok, _ := unstructured.NestedString(ms.Object, "spec", "template", "spec", "providerSpec", "value", "zone")
		Expect(ok).To(BeTrue())

		(*machineSets)[zone] = ms

		return nil
	}
}
