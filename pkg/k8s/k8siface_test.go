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
package k8s_test

import (
	"context"
	"sort"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeFake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Interface", func() {
	Describe("ListNodesWithLabel", testListNodesWithLabel)
	Describe("ListGatewayNodes", testListGatewayNodes)
	Describe("AddGWLabelOnNode", testAddGWLabelOnNode)
	Describe("RemoveGWLabelFromWorkerNodes", testRemoveGWLabelFromWorkerNodes)
})

func testRemoveGWLabelFromWorkerNodes() {
	t := newInterfaceTestDriver()

	BeforeEach(func() {
		t.nodes = []*corev1.Node{
			newNode("node-1", map[string]string{k8s.SubmarinerGatewayLabel: "true"}),
			newNode("node-2", map[string]string{k8s.SubmarinerGatewayLabel: "false"}),
			newNode("node-3", map[string]string{k8s.SubmarinerGatewayLabel: "true", "foo": "bar"}),
		}
	})

	It("should remove the label from all nodes", func() {
		Expect(t.client.RemoveGWLabelFromWorkerNodes()).To(Succeed())
		t.assertNoLabel(t.nodes[0].Name, k8s.SubmarinerGatewayLabel)
		t.assertNoLabel(t.nodes[1].Name, k8s.SubmarinerGatewayLabel)
		t.assertNoLabel(t.nodes[2].Name, k8s.SubmarinerGatewayLabel)
		t.assertLabel(t.nodes[2].Name, "foo", "bar")
	})

	Context("on failure", func() {
		BeforeEach(func() {
			fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes").SetFailOnUpdate(errors.New("fake error"))
		})

		It("should return an error", func() {
			Expect(t.client.RemoveGWLabelFromWorkerNodes()).ToNot(Succeed())
		})
	})
}

func testAddGWLabelOnNode() {
	t := newInterfaceTestDriver()

	BeforeEach(func() {
		t.nodes = []*corev1.Node{newNode("node", map[string]string{"foo": "bar"})}
	})

	When("the gateway label isn't present", func() {
		It("should add it", func() {
			Expect(t.client.AddGWLabelOnNode("node")).To(Succeed())
			t.assertLabel(t.nodes[0].Name, k8s.SubmarinerGatewayLabel, "true")
			t.assertLabel(t.nodes[0].Name, "foo", "bar")
		})
	})

	When("the gateway label is set to false", func() {
		BeforeEach(func() {
			t.nodes[0].Labels[k8s.SubmarinerGatewayLabel] = "false"
		})

		It("should set it to true", func() {
			Expect(t.client.AddGWLabelOnNode(t.nodes[0].Name)).To(Succeed())
			t.assertLabel(t.nodes[0].Name, k8s.SubmarinerGatewayLabel, "true")
		})
	})

	When("the gateway label is already set to true", func() {
		BeforeEach(func() {
			t.nodes[0].Labels[k8s.SubmarinerGatewayLabel] = "true"
		})

		It("should not try to update it", func() {
			Expect(t.client.AddGWLabelOnNode(t.nodes[0].Name)).To(Succeed())

			actualActions := t.kubeClient.Fake.Actions()
			for i := range actualActions {
				if actualActions[i].GetResource().Resource == "nodes" {
					Expect(actualActions[i].GetVerb()).ToNot(Equal("update"))
				}
			}
		})
	})

	When("no labels are present", func() {
		BeforeEach(func() {
			t.nodes[0].Labels = nil
		})

		It("should add the gateway label", func() {
			Expect(t.client.AddGWLabelOnNode(t.nodes[0].Name)).To(Succeed())
			t.assertLabel(t.nodes[0].Name, k8s.SubmarinerGatewayLabel, "true")
		})
	})

	When("the node doesn't exist", func() {
		BeforeEach(func() {
			t.nodes = nil
		})

		It("should not return an error", func() {
			Expect(t.client.AddGWLabelOnNode("node")).To(Succeed())
		})
	})

	Context("on failure", func() {
		BeforeEach(func() {
			fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes").SetFailOnUpdate(errors.New("fake error"))
		})

		It("should return an error", func() {
			Expect(t.client.AddGWLabelOnNode(t.nodes[0].Name)).ToNot(Succeed())
		})
	})
}

func testListGatewayNodes() {
	t := newInterfaceTestDriver()

	BeforeEach(func() {
		t.nodes = []*corev1.Node{
			newNode("node-1", map[string]string{k8s.SubmarinerGatewayLabel: "true"}),
			newNode("node-2", map[string]string{k8s.SubmarinerGatewayLabel: "true"}),
			newNode("node-3", map[string]string{k8s.SubmarinerGatewayLabel: "false"}),
			newNode("node-4", map[string]string{}),
		}
	})

	It("should return the correct nodes", func() {
		list, err := t.client.ListGatewayNodes()
		Expect(err).To(Succeed())

		assertNodeNames(list, "node-1", "node-2")
	})

	Context("on failure", func() {
		BeforeEach(func() {
			fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes").SetFailOnList(errors.New("fake error"))
		})

		It("should return an error", func() {
			_, err := t.client.ListGatewayNodes()
			Expect(err).ToNot(Succeed())
		})
	})
}

func testListNodesWithLabel() {
	t := newInterfaceTestDriver()

	BeforeEach(func() {
		t.nodes = []*corev1.Node{
			newNode("node-1", map[string]string{"label1": "false"}),
			newNode("node-2", map[string]string{"label1": "true"}),
			newNode("node-3", map[string]string{"label2": "false"}),
			newNode("node-4", map[string]string{"label1": "false"}),
		}
	})

	It("should return the correct nodes", func() {
		t.testListNodesWithLabel("label1=false", "node-1", "node-4")
		t.testListNodesWithLabel("label1=true", "node-2")
		t.testListNodesWithLabel("label2=true")
	})

	Context("on failure", func() {
		BeforeEach(func() {
			fake.NewFailingReactorForResource(&t.kubeClient.Fake, "nodes").SetFailOnList(errors.New("fake error"))
		})

		It("should return an error", func() {
			_, err := t.client.ListNodesWithLabel("")
			Expect(err).ToNot(Succeed())
		})
	})
}

type interfaceTestDriver struct {
	kubeClient *kubeFake.Clientset
	nodes      []*corev1.Node
	client     k8s.Interface
}

func newInterfaceTestDriver() *interfaceTestDriver {
	t := &interfaceTestDriver{}

	BeforeEach(func() {
		t.kubeClient = kubeFake.NewSimpleClientset()
	})

	JustBeforeEach(func() {
		for _, node := range t.nodes {
			_, err := t.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
			Expect(err).To(Succeed())
		}

		t.kubeClient.ClearActions()

		t.client = k8s.NewInterface(t.kubeClient)
	})

	return t
}

func (t *interfaceTestDriver) testListNodesWithLabel(labelSelector string, expNodes ...string) {
	list, err := t.client.ListNodesWithLabel(labelSelector)
	Expect(err).To(Succeed())

	assertNodeNames(list, expNodes...)
}

func (t *interfaceTestDriver) assertLabel(name, key, value string) {
	node, err := t.kubeClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).To(Succeed())
	Expect(node.Labels).To(HaveKeyWithValue(key, value))
}

func (t *interfaceTestDriver) assertNoLabel(name, key string) {
	node, err := t.kubeClient.CoreV1().Nodes().Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).To(Succeed())
	Expect(node.Labels).ToNot(HaveKey(key))
}

func assertNodeNames(list *corev1.NodeList, expNodes ...string) {
	actual := []string{}
	nodes := list.Items

	for i := range list.Items {
		actual = append(actual, nodes[i].Name)
	}

	if expNodes == nil {
		expNodes = []string{}
	}

	sort.Strings(actual)
	sort.Strings(expNodes)
	Expect(actual).To(Equal(expNodes))
}

func newNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}
