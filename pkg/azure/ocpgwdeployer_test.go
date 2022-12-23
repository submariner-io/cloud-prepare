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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/azure"
)

func TestAzure(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Azure Suite")
}

var _ = Describe("OCP Gateway Deployer", func() {
	Describe("MachineName", func() {
		It("should be at most 40 characters in length", func() {
			Expect(len(azure.MachineName("acmqe-clc-auto-azure6-fd55b", "centralus", "3"))).To(BeNumerically("<=", 40))
			Expect(len(azure.MachineName("acmqe-clc-auto-azure6-fd55b", "centraleurope", "3"))).To(BeNumerically("<=", 40))
			Expect(len(azure.MachineName("acmqe-clc-auto-azure6-fd55b", "centralus", "bigzone"))).To(BeNumerically("<=", 40))
			Expect(azure.MachineName("acmqe-clc-auto-azure6-fd55b", "us", "1")).To(HavePrefix("acmqe-clc-auto-azure6-fd55b"))
		})
	})
})
