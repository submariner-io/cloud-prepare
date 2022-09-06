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
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
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
		infraID             = "test-infraID"
		machineSetName      = "test-machineset-submariner"
		machineSetNameOther = "test-machineset-other"
	)

	var (
		msClient       dynamic.ResourceInterface
		dynClient      *fakeClient.FakeDynamicClient
		deployer       ocp.MachineSetDeployer
		machineSet     *unstructured.Unstructured
		workerNodeList = []string{infraID + "-worker-b", infraID + "-worker-c", infraID + "-worker-d"}
	)

	BeforeEach(func() {
		machineSet = newMachineSet()
		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(machineSet)

		dynClient = fakeClient.NewSimpleDynamicClient(scheme.Scheme)
		deployer = ocp.NewK8sMachinesetDeployer(restMapper, dynClient)
		msClient = dynClient.Resource(*gvr).Namespace(machineSet.GetNamespace())
	})

	Context("on GetWorkerNodeImage", func() {
		When("no worker node exists", func() {
			It("should return an error", func() {
				_, err := deployer.GetWorkerNodeImage(workerNodeList, machineSet, infraID)
				Expect(err).ToNot(Succeed())
			})
		})

		When("a worker node exists", func() {
			BeforeEach(func() {
				machineSet.SetName(infraID + "-worker-c")
			})

			JustBeforeEach(func() {
				_, err := msClient.Create(context.TODO(), machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			Context("", func() {
				BeforeEach(func() {
					disks := []interface{}{
						map[string]interface{}{
							"image": "some-image",
						},
					}

					_ = unstructured.SetNestedSlice(machineSet.Object, disks, "spec", "template", "spec", "providerSpec", "value", "disks")
				})

				It("should return its disk image", func() {
					image, err := deployer.GetWorkerNodeImage(workerNodeList, machineSet, infraID)
					Expect(err).To(Succeed())
					Expect(image).To(Equal("some-image"))
				})
			})

			Context("and has no disks", func() {
				It("should return an error", func() {
					_, err := deployer.GetWorkerNodeImage(workerNodeList, machineSet, infraID)
					Expect(err).ToNot(Succeed())
				})
			})

			Context("and retrieval fails", func() {
				var expectedErr error

				BeforeEach(func() {
					expectedErr = errors.New("fake Get error")
					fake.NewFailingReactor(&dynClient.Fake).SetFailOnGet(expectedErr)
				})

				It("should return an error", func() {
					_, err := deployer.GetWorkerNodeImage(workerNodeList, machineSet, infraID)
					Expect(err).To(ContainErrorSubstring(expectedErr))
				})
			})
		})
	})

	Context("on Deploy", func() {
		BeforeEach(func() {
			machineSet.SetName(machineSetName)
		})

		It("should successfully create the machine set", func() {
			Expect(deployer.Deploy(machineSet)).To(Succeed())

			_, err := msClient.Get(context.TODO(), machineSetName, metav1.GetOptions{})
			Expect(err).To(Succeed())
		})
	})

	Context("on Delete", func() {
		BeforeEach(func() {
			machineSet.SetName(machineSetName)
		})

		When("the machine set exists", func() {
			BeforeEach(func() {
				_, err := msClient.Create(context.TODO(), machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			It("should successfully delete the machine set", func() {
				Expect(deployer.Delete(machineSet)).To(Succeed())

				_, err := msClient.Get(context.TODO(), machineSetName, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})

			Context("and deletion fails", func() {
				BeforeEach(func() {
					fake.NewFailingReactor(&dynClient.Fake).SetFailOnDelete(errors.New("fake Delete error"))
				})

				It("should return an error", func() {
					Expect(deployer.Delete(machineSet)).ToNot(Succeed())
				})
			})
		})

		When("the machine set does not exist", func() {
			It("should not return an error", func() {
				Expect(deployer.Delete(machineSet)).To(Succeed())
			})
		})
	})

	Context("on List", func() {
		When("matching and non-matching machine sets exist", func() {
			BeforeEach(func() {
				machineSet.SetName(machineSetName)
				_, err := msClient.Create(context.TODO(), machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
				machineSet.SetName(machineSetNameOther)
				_, err = msClient.Create(context.TODO(), machineSet, metav1.CreateOptions{})
				Expect(err).To(Succeed())
			})

			It("should return only the matching machine set", func() {
				machineSetList, err := deployer.List(machineSet, "submariner")
				Expect(err).To(Succeed())

				Expect(len(machineSetList)).To(Equal(1))
				Expect(machineSetList[0].GetName()).To(Equal(machineSetName))
			})
		})

		When("a matching machine set does not exist", func() {
			It("should not return an error", func() {
				machineSetList, err := deployer.List(machineSet, "submariner")
				Expect(err).To(Succeed())
				Expect(len(machineSetList)).To(Equal(0))
			})
		})
	})
})

func newMachineSet() *unstructured.Unstructured {
	ms := &unstructured.Unstructured{}
	ms.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "machine.openshift.io",
		Version: "v1beta1",
		Kind:    "MachineSet",
	})
	ms.SetNamespace("test-ns")

	return ms
}
