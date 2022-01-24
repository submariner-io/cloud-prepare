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
package aws

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

var _ = Describe("Cloud", func() {
	Context("CreateVpcPeering", testCreateVpcPeering)
})

func testCreateVpcPeering() {
	cloudA := newCloudTestDriver(infraID, region)
	When("called with a non-AWS Cloud", func() {
		It("should return an error", func() {
			invalidCloud := &invalidCloud{}
			err := cloudA.cloud.CreateVpcPeering(invalidCloud, api.NewLoggingReporter())
			Expect(err).To(HaveOccurred())
		})
	})
}

type invalidCloud struct{}

func (f *invalidCloud) PrepareForSubmariner(input api.PrepareForSubmarinerInput, reporter api.Reporter) error {
	panic("not implemented")
}

func (f *invalidCloud) CreateVpcPeering(target api.Cloud, reporter api.Reporter) error {
	panic("not implemented")
}

func (f *invalidCloud) CleanupAfterSubmariner(reporter api.Reporter) error {
	panic("not implemented")
}

type cloudTestDriver struct {
	fakeAWSClientBase
	cloud api.Cloud
}

func newCloudTestDriver(infraID, region string) *cloudTestDriver {
	t := &cloudTestDriver{}

	BeforeEach(func() {
		t.beforeEach()
		t.cloud = NewCloud(t.awsClient, infraID, region)
	})

	AfterEach(t.afterEach)

	return t
}
