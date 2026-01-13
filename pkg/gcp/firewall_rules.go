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

package gcp

import (
	"fmt"
	"strconv"

	"github.com/submariner-io/cloud-prepare/pkg/api"
	"google.golang.org/api/compute/v1"
)

const (
	ingressDirection         = "INGRESS"
	publicPortsRuleName      = "submariner-public-ports"
	internalPortsRuleName    = "submariner-internal-ports"
	submarinerGatewayNodeTag = "submariner-io-gateway-node"
)

func newExternalFirewallRules(projectID, infraID, network string, ports []api.PortSpec) *compute.Firewall {
	ingressName := generateRuleName(infraID, publicPortsRuleName)

	// We want the external firewall rules to be applied only to Gateway nodes. So, we use the TargetTags
	// field and include submarinerGatewayNodeTag for selection of Gateway nodes. All the Submariner Gateway
	// instances will be tagged with submarinerGatewayNodeTag.
	ingressRule := newFirewallRule(projectID, ingressName, ingressDirection, network, ports)
	ingressRule.TargetTags = []string{
		submarinerGatewayNodeTag,
	}

	return ingressRule
}

func newInternalFirewallRule(projectID, infraID, network string, ports []api.PortSpec) *compute.Firewall {
	ingressName := generateRuleName(infraID, internalPortsRuleName)

	rule := newFirewallRule(projectID, ingressName, ingressDirection, network, ports)
	rule.TargetTags = []string{
		infraID + "-worker",
		infraID + "-master",
	}
	rule.SourceTags = []string{
		infraID + "-worker",
		infraID + "-master",
	}

	return rule
}

func newFirewallRule(projectID, name, direction, network string, ports []api.PortSpec) *compute.Firewall {
	allowedPorts := make([]*compute.FirewallAllowed, 0, len(ports))

	for _, port := range ports {
		fwRule := &compute.FirewallAllowed{
			IPProtocol: port.Protocol,
		}
		if port.Port != 0 {
			fwRule.Ports = []string{strconv.Itoa(int(port.Port))}
		}

		allowedPorts = append(allowedPorts, fwRule)
	}

	return &compute.Firewall{
		Name:      name,
		Network:   fmt.Sprintf("projects/%s/global/networks/%s", projectID, network),
		Direction: direction,
		Allowed:   allowedPorts,
	}
}

func generateRuleName(infraID, name string) string {
	return fmt.Sprintf("%s-%s-ingress", infraID, name)
}
