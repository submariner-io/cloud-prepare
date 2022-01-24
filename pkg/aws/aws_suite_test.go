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
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/aws/client/fake"
)

const (
	infraID = "test-infraID"
	region  = "test-region"

	targetInfraID = "other-infraID"
	targetRegion  = "other-region"
)

func TestAWS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AWS Suite")
}

type fakeAWSClientBase struct {
	awsClient *fake.MockInterface
	mockCtrl  *gomock.Controller
}

func (f *fakeAWSClientBase) beforeEach() {
	f.mockCtrl = gomock.NewController(GinkgoT())
	f.awsClient = fake.NewMockInterface(f.mockCtrl)
}

func (f *fakeAWSClientBase) afterEach() {
	f.mockCtrl.Finish()
}
