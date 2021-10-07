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
	"strings"

	"github.com/submariner-io/cloud-prepare/pkg/api"
)

type gcpCloud struct {
	CloudInfo
}

// NewCloud creates a new api.Cloud instance which can prepare GCP for Submariner to be deployed on it
func NewCloud(info CloudInfo) api.Cloud {
	return &gcpCloud{CloudInfo: info}
}

// PrepareForSubmariner prepares submariner cluster environment on GCP
func (gc *gcpCloud) PrepareForSubmariner(input api.PrepareForSubmarinerInput, reporter api.Reporter) error {
	// create the inbound firewall rule for submariner internal ports
	reporter.Started("Opening internal ports %q for intra-cluster communications on GCP", formatPorts(input.InternalPorts))
	internalIngress := newInternalFirewallRule(gc.ProjectID, gc.InfraID, input.InternalPorts)
	if err := gc.openPorts(internalIngress); err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Opened internal ports %q with firewall rule %q on GCP",
		formatPorts(input.InternalPorts), internalIngress.Name)

	return nil
}

// CleanupAfterSubmariner clean up submariner cluster environment on GCP
func (gc *gcpCloud) CleanupAfterSubmariner(reporter api.Reporter) error {
	// delete the inbound and outbound firewall rules to close submariner internal ports
	internalIngressName := generateRuleName(gc.InfraID, internalPortsRuleName)

	return gc.deleteFirewallRule(internalIngressName, reporter)
}

func formatPorts(ports []api.PortSpec) string {
	portStrs := []string{}
	for _, port := range ports {
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}

	return strings.Join(portStrs, ", ")
}

type gcpGatewayDeployer struct {
	CloudInfo
}

// NewGCPGatewayDeployer created a GatewayDeployer capable of deploying gateways to GCP
func NewGCPGatewayDeployer(info CloudInfo) api.GatewayDeployer {
	return &gcpGatewayDeployer{CloudInfo: info}
}

func (d *gcpGatewayDeployer) Deploy(input api.GatewayDeployInput, reporter api.Reporter) error {
	// create the inbound and outbound firewall rules for submariner public ports
	reporter.Started("Opening public ports %q for cluster communications on GCP", formatPorts(input.PublicPorts))
	ingress := newExternalFirewallRules(d.ProjectID, d.InfraID, input.PublicPorts)
	if err := d.openPorts(ingress); err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Opened public ports %q with firewall rules %q on GCP",
		formatPorts(input.PublicPorts), ingress.Name)

	return nil
}

func (d *gcpGatewayDeployer) Cleanup(reporter api.Reporter) error {
	// delete the inbound and outbound firewall rules to close submariner public ports
	ingressName := generateRuleName(d.InfraID, publicPortsRuleName)

	if err := d.deleteFirewallRule(ingressName, reporter); err != nil {
		return err
	}

	return nil
}
