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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	azcloud "github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v10"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/azure"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

const (
	baseGroupName = "base-group"
	testInfraID   = "test-infra"
	testRegion    = "test-region"
)

var SKUsPath = ResourcePath{NoGroup: true, Type: "skus"}

func TestAzure(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Azure Suite")
}

type ResourcePath struct {
	NoGroup bool
	Type    string
	Name    string
}
type testDriver struct {
	kubeClient       *k8sfake.Clientset
	cloudInfo        azure.CloudInfo
	httpHandler      http.HandlerFunc
	httpGetResponses map[ResourcePath]any
	httpPutRespCodes map[ResourcePath]*int
	httpPutRequests  map[string][]byte
}

func newTestDriver() *testDriver {
	t := &testDriver{}

	BeforeEach(func() {
		t.kubeClient = k8sfake.NewClientset()
		t.httpGetResponses = map[ResourcePath]any{}
		t.httpPutRespCodes = map[ResourcePath]*int{}
		t.httpPutRequests = map[string][]byte{}

		t.cloudInfo = azure.CloudInfo{
			SubscriptionID: "123e4567-e89b-12d3-a456-426614174000",
			InfraID:        testInfraID,
			Region:         testRegion,
			BaseGroupName:  baseGroupName,
			K8sClient:      k8s.NewInterface(t.kubeClient),
		}

		t.httpHandler = func(w http.ResponseWriter, req *http.Request) {
			defer GinkgoRecover()

			Expect(req.URL.Path).To(HavePrefix("/subscriptions/" + t.cloudInfo.SubscriptionID))

			findGetRespEntry := func() (ResourcePath, any) {
				return findPathEntry(t.httpGetResponses, req.URL.Path, func(path ResourcePath, s string) bool {
					return pathsMatch(s, path)
				})
			}

			switch req.Method {
			case http.MethodGet:
				_, resp := findGetRespEntry()
				if statusCode, ok := resp.(int); ok {
					w.WriteHeader(statusCode)
				} else if resp != nil {
					var (
						bytes []byte
						err   error
					)

					if bytes, ok = resp.([]byte); !ok {
						bytes, err = json.Marshal(resp)
						Expect(err).NotTo(HaveOccurred())
					}

					_, err = w.Write(bytes)
					Expect(err).NotTo(HaveOccurred())
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			case http.MethodPut:
				_, statusCode := findPathEntry(t.httpPutRespCodes, req.URL.Path, func(path ResourcePath, s string) bool {
					return pathsMatch(s, path)
				})
				if statusCode != nil {
					w.WriteHeader(*statusCode)
					return
				}

				defer req.Body.Close()

				bodyBytes, err := io.ReadAll(req.Body)
				Expect(err).NotTo(HaveOccurred())

				t.httpPutRequests[req.URL.Path] = bodyBytes
				t.httpGetResponses[toResourcePath(req.URL.Path)] = bodyBytes
			case http.MethodDelete:
				key, _ := findGetRespEntry()
				delete(t.httpGetResponses, key)
			}
		}
	})

	JustBeforeEach(func() {
		server := httptest.NewServer(t.httpHandler)
		DeferCleanup(server.Close)

		t.cloudInfo.ClientOptions = &arm.ClientOptions{
			ClientOptions: policy.ClientOptions{
				Cloud: azcloud.Configuration{
					Services: map[azcloud.ServiceName]azcloud.ServiceConfiguration{
						azcloud.ResourceManager: {
							Endpoint: server.URL,
							Audience: "public",
						},
					},
				},
			},
		}
	})

	return t
}

func (t *testDriver) assertPutRequest(path ResourcePath, objType any) {
	_, bytes := findPathEntry(t.httpPutRequests, path, pathsMatch)

	Expect(bytes).NotTo(BeEmpty(), "No PUT request received for %#v", path)
	Expect(json.Unmarshal(bytes, objType)).NotTo(HaveOccurred())
}

func (t *testDriver) assertNoPutRequest(path ResourcePath) {
	_, value := findPathEntry(t.httpPutRequests, path, pathsMatch)
	Expect(value).To(BeNil(), "Unexpected PUT request received for %#v", path)
}

func findPathEntry[K comparable, V any, P any](from map[K]V, path P, matches func(K, P) bool) (K, V) {
	var (
		resultValue V
		resultKey   K
	)

	for k, v := range from {
		if matches(k, path) {
			resultValue = v
			resultKey = k

			break
		}
	}

	return resultKey, resultValue
}

func pathsMatch(urlPath string, rp ResourcePath) bool {
	containsGroup := strings.Contains(urlPath, "/resourceGroups/"+baseGroupName)
	if rp.NoGroup != !containsGroup {
		return false
	}

	typePath := "/" + rp.Type
	if rp.Name != "" {
		typePath += "/" + rp.Name
	}

	return strings.HasSuffix(urlPath, typePath)
}

func toResourcePath(urlPath string) ResourcePath {
	s := strings.Split(urlPath, "/")

	return ResourcePath{
		Type:    s[len(s)-2],
		Name:    s[len(s)-1],
		NoGroup: !strings.Contains(urlPath, "/resourceGroups/"),
	}
}

func securityGroupPath(name string) ResourcePath {
	return ResourcePath{Type: "networkSecurityGroups", Name: name}
}

func publicAddressesPath(name string) ResourcePath {
	return ResourcePath{Type: "publicIPAddresses", Name: name}
}

func networkInterfacesPath(name string) ResourcePath {
	return ResourcePath{Type: "networkInterfaces", Name: name}
}

func securityRuleMatchers(port api.PortSpec, rulePrefix string) []types.GomegaMatcher {
	return []types.GomegaMatcher{
		Satisfy(func(r *armnetwork.SecurityRule) bool {
			return strings.HasPrefix(ptr.Deref(r.Name, ""), rulePrefix) &&
				string(ptr.Deref(r.Properties.Protocol, "")) == port.Protocol &&
				ptr.Deref(r.Properties.DestinationPortRange, "") == fmt.Sprintf("%d-%d", port.Port, port.Port) &&
				ptr.Deref(r.Properties.Direction, "") == armnetwork.SecurityRuleDirectionInbound
		}),
		Satisfy(func(r *armnetwork.SecurityRule) bool {
			return strings.HasPrefix(ptr.Deref(r.Name, ""), rulePrefix) &&
				string(ptr.Deref(r.Properties.Protocol, "")) == port.Protocol &&
				ptr.Deref(r.Properties.DestinationPortRange, "") == fmt.Sprintf("%d-%d", port.Port, port.Port) &&
				ptr.Deref(r.Properties.Direction, "") == armnetwork.SecurityRuleDirectionOutbound
		}),
	}
}
