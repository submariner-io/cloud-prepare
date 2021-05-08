/*
© 2021 Red Hat, Inc. and others.

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
	gcpclient "github.com/submariner-io/cloud-prepare/pkg/gcp/client"

	"google.golang.org/api/compute/v1"
)

type gcpCloud struct {
	infraID   string
	projectID string
	client    gcpclient.Interface
}

// NewCloud creates a new api.Cloud instance which can prepare GCP for Submariner to be deployed on it
func NewCloud(projectID, infraID string, client gcpclient.Interface) api.Cloud {
	return &gcpCloud{
		infraID:   infraID,
		projectID: projectID,
		client:    client,
	}
}

// PrepareForSubmariner prepares submariner cluster environment on GCP
func (gc *gcpCloud) PrepareForSubmariner(input api.PrepareForSubmarinerInput, reporter api.Reporter) error {
	// create the inbound and outbound firewall rules for submariner public ports
	reporter.Started("Opening public ports %q for cluster communications on GCP", formatPorts(input.PublicPorts))
	ingress, egress := newExternalFirewallRules(gc.projectID, gc.infraID, input.PublicPorts)
	if err := gc.openPorts(ingress, egress); err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Opened public ports %q with firewall rules %q and %q on GCP",
		formatPorts(input.PublicPorts), ingress.Name, egress.Name)

	// create the inbound firewall rule for submariner internal ports
	reporter.Started("Opening internal ports %q for intra-cluster communications on GCP", formatPorts(input.InternalPorts))
	internalIngress := newInternalFirewallRule(gc.projectID, gc.infraID, input.InternalPorts)
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
	// delete the inbound and outbound firewall rules to close submariner public ports
	ingressName, egressName := generateRuleNames(gc.infraID, publicPortsRuleName)

	if err := gc.deleteFirewallRule(ingressName, reporter); err != nil {
		return err
	}

	if err := gc.deleteFirewallRule(egressName, reporter); err != nil {
		return err
	}

	// delete the inbound and outbound firewall rules to close submariner internal ports
	internalIngressName, _ := generateRuleNames(gc.infraID, internalPortsRuleName)

	return gc.deleteFirewallRule(internalIngressName, reporter)
}

// open expected ports by creating related firewall rule
// - if the firewall rule is not found, we will create it
// - if the firewall rule is found and changed, we will update it
func (gc *gcpCloud) openPorts(rules ...*compute.Firewall) error {
	for _, rule := range rules {
		_, err := gc.client.GetFirewallRule(gc.projectID, rule.Name)
		if gcpclient.IsGCPNotFoundError(err) {
			if err := gc.client.InsertFirewallRule(gc.projectID, rule); err != nil {
				return err
			}

			continue
		}

		if err != nil {
			return err
		}

		if err := gc.client.UpdateFirewallRule(gc.projectID, rule.Name, rule); err != nil {
			return err
		}
	}

	return nil
}

func (gc *gcpCloud) deleteFirewallRule(name string, reporter api.Reporter) error {
	reporter.Started("Deleting firewall rule %q on GCP", name)

	if err := gc.client.DeleteFirewallRule(gc.projectID, name); err != nil {
		if !gcpclient.IsGCPNotFoundError(err) {
			reporter.Failed(err)
			return err
		}
	}

	reporter.Succeeded("Deleted firewall rule %q on GCP", name)

	return nil
}

func formatPorts(ports []api.PortSpec) string {
	portStrs := []string{}
	for _, port := range ports {
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}

	return strings.Join(portStrs, ", ")
}
