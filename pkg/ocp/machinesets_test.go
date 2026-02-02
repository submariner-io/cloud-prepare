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

package ocp_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("K8s MachineSetDeployer", func() {
	const (
		infraID        = "test-infraID"
		machineSetName = "test-machineset-submariner"
	)

	var (
		msClient   dynamic.ResourceInterface
		dynClient  *fakeClient.FakeDynamicClient
		deployer   ocp.MachineSetDeployer
		machineSet *unstructured.Unstructured
	)

	BeforeEach(func() {
		machineSet = newMachineSet("true")
		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(machineSet)

		dynClient = fakeClient.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, map[schema.GroupVersionResource]string{
			*gvr: "MachineSetList",
		})
		deployer = ocp.NewK8sMachinesetDeployer(restMapper, dynClient)
		msClient = dynClient.Resource(*gvr).Namespace(machineSet.GetNamespace())
	})

	Context("on GetWorkerNodeImage", func() {
		When("no worker node exists", func() {
			It("should return an error", func(ctx SpecContext) {
				_, err := deployer.GetWorkerNodeImage(ctx, machineSet, infraID)
				Expect(err).ToNot(Succeed())
			})
		})

		When("a worker node exists", func() {
			BeforeEach(func() {
				machineSet.SetName(infraID + "-worker-c")
			})

			JustBeforeEach(func(ctx SpecContext) {
				_, err := msClient.Create(ctx, machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			Context("", func() {
				BeforeEach(func() {
					disks := []any{
						map[string]any{
							"image": "some-image",
						},
					}

					_ = unstructured.SetNestedSlice(machineSet.Object, disks, "spec", "template", "spec", "providerSpec", "value", "disks")
				})

				It("should return its disk image", func(ctx SpecContext) {
					image, err := deployer.GetWorkerNodeImage(ctx, machineSet, infraID)
					Expect(err).To(Succeed())
					Expect(image).To(Equal("some-image"))
				})
			})

			Context("and has no disks", func() {
				It("should return an error", func(ctx SpecContext) {
					_, err := deployer.GetWorkerNodeImage(ctx, machineSet, infraID)
					Expect(err).ToNot(Succeed())
				})
			})

			Context("and retrieval fails", func() {
				var expectedErr error

				BeforeEach(func() {
					expectedErr = errors.New("fake List error")
					fake.NewFailingReactor(&dynClient.Fake).SetFailOnList(expectedErr)
				})

				It("should return an error", func(ctx SpecContext) {
					_, err := deployer.GetWorkerNodeImage(ctx, machineSet, infraID)
					Expect(err).To(ContainErrorSubstring(expectedErr))
				})
			})
		})
	})

	Context("on Deploy", func() {
		BeforeEach(func() {
			machineSet.SetName(machineSetName)
		})

		It("should successfully create the machine set", func(ctx SpecContext) {
			Expect(deployer.Deploy(ctx, machineSet)).To(Succeed())

			_, err := msClient.Get(ctx, machineSetName, metav1.GetOptions{})
			Expect(err).To(Succeed())
		})
	})

	Context("on Delete", func() {
		BeforeEach(func() {
			machineSet.SetName(machineSetName)
		})

		When("the machine set exists", func() {
			BeforeEach(func(ctx SpecContext) {
				_, err := msClient.Create(ctx, machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			It("should successfully delete the machine set", func(ctx SpecContext) {
				Expect(deployer.Delete(ctx, machineSet)).To(Succeed())

				_, err := msClient.Get(ctx, machineSetName, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})

			Context("and deletion fails", func() {
				BeforeEach(func() {
					fake.NewFailingReactor(&dynClient.Fake).SetFailOnDelete(errors.New("fake Delete error"))
				})

				It("should return an error", func(ctx SpecContext) {
					Expect(deployer.Delete(ctx, machineSet)).ToNot(Succeed())
				})
			})
		})

		When("the machine set does not exist", func() {
			It("should not return an error", func(ctx SpecContext) {
				Expect(deployer.Delete(ctx, machineSet)).To(Succeed())
			})
		})
	})

	Context("on List", func() {
		When("matching and non-matching machine sets exist", func() {
			BeforeEach(func(ctx SpecContext) {
				machineSet.SetName(machineSetName)
				_, err := msClient.Create(ctx, machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
				machineSet = newMachineSet("false")
				_, err = msClient.Create(ctx, machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			It("should return only the matching machine set", func(ctx SpecContext) {
				machineSetList, err := deployer.List(ctx)
				Expect(err).To(Succeed())

				Expect(machineSetList).To(HaveLen(1))
				Expect(machineSetList[0].GetName()).To(Equal(machineSetName))
			})
		})

		When("a matching machine set does not exist", func() {
			It("should not return an error", func(ctx SpecContext) {
				machineSetList, err := deployer.List(ctx)
				Expect(err).To(Succeed())
				Expect(machineSetList).To(BeEmpty())
			})
		})
	})
})

func newMachineSet(isGateway string) *unstructured.Unstructured {
	ms := &unstructured.Unstructured{}
	ms.SetUnstructuredContent(map[string]any{
		"apiVersion": "machine.openshift.io/v1beta1",
		"kind":       "MachineSet",
		"metadata": map[string]any{
			"namespace": "test-ns",
		},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							ocp.SubmarinerGatewayLabel: isGateway,
						},
					},
				},
			},
		},
	})

	return ms
}
