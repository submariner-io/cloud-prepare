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

package azure

import (
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/util"
	ocpFake "github.com/submariner-io/cloud-prepare/pkg/ocp/fake"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("OCP Gateway Deployer", func() {
	const (
		infraID      = "test-infraID"
		region       = "east"
		image        = "test-image"
		zone         = "east-zone"
		instanceType = "large"
	)

	var (
		mockCtrl   *gomock.Controller
		msDeployer *ocpFake.MockMachineSetDeployer
		gwDeployer *ocpGatewayDeployer
		machineSet *unstructured.Unstructured
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		msDeployer = ocpFake.NewMockMachineSetDeployer(mockCtrl)

		info := &CloudInfo{
			InfraID: infraID,
			Region:  region,
		}

		gwp, err := NewOcpGatewayDeployer(info, NewCloud(info), msDeployer, instanceType)
		Expect(err).To(Succeed())

		gwDeployer = gwp.(*ocpGatewayDeployer)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("deployGateway", func() {
		JustBeforeEach(func() {
			msDeployer.EXPECT().Deploy(gomock.Any()).DoAndReturn(func(ms *unstructured.Unstructured) error {
				machineSet = ms
				return nil
			}).AnyTimes()
		})

		It("should deploy the correct MachineSet", func() {
			Expect(gwDeployer.deployGateway(zone, image, false)).To(Succeed())

			Expect(machineSet).ToNot(BeNil())
			Expect(machineSet.GetLabels()).To(HaveKeyWithValue("machine.openshift.io/cluster-api-cluster", infraID))
			Expect(util.GetNestedField(machineSet, "metadata", "name")).To(HavePrefix(submarinerGatewayGW + region))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "metadata", "labels")).
				To(HaveKeyWithValue("submariner.io/gateway", "true"))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "image", "resourceID")).To(Equal(image))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "location")).To(Equal(region))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "zone")).To(Equal(zone))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "vmSize")).To(Equal(instanceType))
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "publicIP")).To(BeTrue())

			machineSet = nil
			Expect(gwDeployer.deployGateway(zone, image, true)).To(Succeed())

			Expect(machineSet).ToNot(BeNil())
			Expect(util.GetNestedField(machineSet, "spec", "template", "spec", "providerSpec", "value", "publicIP")).To(BeFalse())
		})
	})
})
