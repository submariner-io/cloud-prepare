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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/gcp/client/fake"
	"go.uber.org/mock/gomock"
)

const (
	infraID      = "test-infraID"
	region       = "test-region"
	projectID    = "test-projectID"
	instanceType = "test-instance-type"
	zone1        = "test-zone1"
	zone2        = "test-zone2"
	instance1    = infraID + "instance1"
	instance2    = infraID + "instance2"
)

func TestGCP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GCP Suite")
}

type fakeGCPClientBase struct {
	gcpClient *fake.MockInterface
	mockCtrl  *gomock.Controller
}

func (f *fakeGCPClientBase) beforeEach() {
	f.mockCtrl = gomock.NewController(GinkgoT())
	f.gcpClient = fake.NewMockInterface(f.mockCtrl)
}

func (f *fakeGCPClientBase) afterEach() {
	f.mockCtrl.Finish()
}
