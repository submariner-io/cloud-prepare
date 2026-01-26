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
	"context"
	"fmt"
	"strings"

	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

type gcpCloud struct {
	CloudInfo
}

// NewCloud creates a new api.Cloud instance which can prepare GCP for Submariner to be deployed on it.
func NewCloud(info CloudInfo, //nolint: gocritic //Ignore 'hugeParam' - pass by value for CloudInfo is intentional.
) api.Cloud {
	gcpCloud := &gcpCloud{CloudInfo: info}

	if gcpCloud.VpcName == "" {
		gcpCloud.VpcName = info.InfraID + "-network"
	}

	if gcpCloud.PublicSubnetName == "" {
		gcpCloud.PublicSubnetName = info.InfraID + "-worker-subnet"
	}

	return gcpCloud
}

func (gc *gcpCloud) OpenPorts(_ context.Context, ports []api.PortSpec, status reporter.Interface) error {
	// Create the inbound firewall rule for submariner internal ports.
	status.Start("Opening internal ports %q for intra-cluster communications on GCP", formatPorts(ports))
	defer status.End()

	internalIngress := newInternalFirewallRule(gc.ProjectID, gc.InfraID, gc.VpcName, ports)
	if err := gc.openPorts(internalIngress); err != nil {
		return status.Error(err, "unable to open ports")
	}

	status.Success("Opened internal ports %q with firewall rule %q on GCP",
		formatPorts(ports), internalIngress.Name)

	return nil
}

func (gc *gcpCloud) ClosePorts(_ context.Context, status reporter.Interface) error {
	// Delete the inbound and outbound firewall rules to close submariner internal ports.
	internalIngressName := generateRuleName(gc.InfraID, internalPortsRuleName)

	return gc.deleteFirewallRule(internalIngressName, status)
}

func formatPorts(ports []api.PortSpec) string {
	portStrs := make([]string, 0, len(ports))
	for _, port := range ports {
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}

	return strings.Join(portStrs, ", ")
}
