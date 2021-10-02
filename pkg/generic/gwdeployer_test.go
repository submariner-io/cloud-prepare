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
package generic_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/generic"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeFake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("GatewayDeployer", func() {
	t := newGatewayDeployerTestDriver()

	When("one gateway is requested", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newMasterNode("master-node"),
				newNonMasterNode("node-1"),
			}

			t.numGateways = 1
		})

		It("should label one gateway node", func() {
			Expect(t.doDeploy()).To(Succeed())
			t.awaitLabeledNodes(1)
		})

		Context("and there are no worker nodes", func() {
			BeforeEach(func() {
				t.nodes = []*corev1.Node{}
			})

			It("return an error", func() {
				Expect(t.doDeploy()).ToNot(Succeed())
			})
		})
	})

	When("two gateways are requested", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
				newMasterNode("master-node"),
				newNonMasterNode("node-2"),
			}

			t.numGateways = 2
		})

		It("should label two gateway nodes", func() {
			Expect(t.doDeploy()).To(Succeed())
			t.awaitLabeledNodes(2)
		})

		Context("and there's an insufficient number of worker nodes", func() {
			BeforeEach(func() {
				t.nodes = []*corev1.Node{
					newNonMasterNode("node-1"),
					newMasterNode("master-node"),
				}
			})

			It("should partially label the gateways and return an error", func() {
				Expect(t.doDeploy()).ToNot(Succeed())
				t.awaitLabeledNodes(1)
			})
		})
	})

	When("the requested number of gateways is increased", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
				newNonMasterNode("node-2"),
			}

			setGWLabel(t.nodes[0])
			t.numGateways = 2
		})

		It("should label the additional gateway nodes", func() {
			Expect(t.doDeploy()).To(Succeed())
			t.awaitLabeledNodes(2)
		})
	})

	When("the requested number of gateway nodes are already labeled", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
				newNonMasterNode("node-2"),
			}

			setGWLabel(t.nodes[0])
			setGWLabel(t.nodes[1])
			t.numGateways = 2
		})

		It("should not try to update them", func() {
			Expect(t.doDeploy()).To(Succeed())

			actualActions := t.kubeClient.Fake.Actions()
			for i := range actualActions {
				if actualActions[i].GetResource().Resource == "nodes" {
					Expect(actualActions[i].GetVerb()).ToNot(Equal("update"))
				}
			}

		})
	})

	When("labeling a node fails", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
			}

			t.numGateways = 1

			reactor := fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes")
			reactor.SetFailOnUpdate(errors.New("fake error"))
		})

		It("should return an error", func() {
			Expect(t.doDeploy()).ToNot(Succeed())
		})
	})

	When("the requested number of gateways is decreased", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
				newNonMasterNode("node-2"),
			}

			setGWLabel(t.nodes[0])
			setGWLabel(t.nodes[1])
			t.numGateways = 1
		})

		It("should not unlabel the subtracted gateway nodes", func() {
			Expect(t.doDeploy()).To(Succeed())
			t.awaitLabeledNodes(2)
		})
	})

	Context("on clean up", func() {
		BeforeEach(func() {
			t.nodes = []*corev1.Node{
				newNonMasterNode("node-1"),
				newNonMasterNode("node-2"),
			}

			setGWLabel(t.nodes[0])
			setGWLabel(t.nodes[1])
		})

		It("should unlabel all gateway nodes", func() {
			Expect(t.gwDeployer.Cleanup(api.NewLoggingReporter())).To(Succeed())
			t.awaitLabeledNodes(0)
		})

		When("unlabeling a node fails", func() {
			BeforeEach(func() {
				reactor := fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes")
				reactor.SetFailOnUpdate(errors.New("fake error"))
			})

			It("should return an error", func() {
				Expect(t.gwDeployer.Cleanup(api.NewLoggingReporter())).ToNot(Succeed())
			})
		})
	})
})

type gatewayDeployerTestDriver struct {
	numGateways int
	kubeClient  *kubeFake.Clientset
	nodes       []*corev1.Node
	gwDeployer  api.GatewayDeployer
}

func newGatewayDeployerTestDriver() *gatewayDeployerTestDriver {
	t := &gatewayDeployerTestDriver{}

	BeforeEach(func() {
		t.nodes = []*corev1.Node{}

		t.kubeClient = kubeFake.NewSimpleClientset()
	})

	JustBeforeEach(func() {
		for _, node := range t.nodes {
			_, err := t.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
			Expect(err).To(Succeed())
		}

		t.kubeClient.ClearActions()

		t.gwDeployer = generic.NewGatewayDeployer(k8s.NewInterface(t.kubeClient))
	})

	return t
}

func (t *gatewayDeployerTestDriver) getLabeledWorkerNodes() []*corev1.Node {
	foundNodes := []*corev1.Node{}

	for _, expected := range t.nodes {
		actual, err := t.kubeClient.CoreV1().Nodes().Get(context.TODO(), expected.Name, metav1.GetOptions{})
		Expect(err).To(Succeed())

		if _, ok := actual.Labels["node-role.kubernetes.io/master"]; ok {
			continue
		}

		if actual.Labels["submariner.io/gateway"] == "true" {
			foundNodes = append(foundNodes, actual)
		}
	}

	return foundNodes
}

func (t *gatewayDeployerTestDriver) awaitLabeledNodes(expCount int) {
	Eventually(func() int {
		return len(t.getLabeledWorkerNodes())
	}, 2).Should(Equal(expCount), "The expected number of nodes weren't labeled")
}

func (t *gatewayDeployerTestDriver) doDeploy() error {
	return t.gwDeployer.Deploy(api.GatewayDeployInput{Gateways: t.numGateways}, api.NewLoggingReporter())
}

func newNonMasterNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
	}
}

func newMasterNode(name string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    "node-role.kubernetes.io/master",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
}

func setGWLabel(node *corev1.Node) {
	node.Labels["submariner.io/gateway"] = "true"
}
