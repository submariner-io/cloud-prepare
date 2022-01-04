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
package rhos

import (
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

type ocpGatewayDeployer struct {
	CloudInfo
	projectID string
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable of deploying gateways using OCP
// If the supplied cloud is not a RHOS, an error is returned.
func NewOcpGatewayDeployer(info CloudInfo, projectID string) api.GatewayDeployer {
	return &ocpGatewayDeployer{
		CloudInfo: info,
		projectID: projectID,
	}
}

func (d *ocpGatewayDeployer) Deploy(input api.GatewayDeployInput, reporter api.Reporter) error {
	reporter.Started("Configuring the required firewall rules for inter-cluster traffic")

	computeClient, err := openstack.NewComputeV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return errors.WithMessage(err, "error creating the compute client")
	}

	networkClient, err := openstack.NewNetworkV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return errors.WithMessage(err, "error creating the network client")
	}

	groupName := d.InfraID + gwSecurityGroupSuffix
	if err := d.createGWSecurityGroup(input.PublicPorts, groupName, computeClient, networkClient); err != nil {
		return errors.WithMessage(err, "creating gateway security group failed")
	}

	reporter.Succeeded("Opened External ports %q in security group %q on RHOS",
		formatPorts(input.PublicPorts), groupName)

	reporter.Started("Configuring the required number of gateway pods")

	gwNodes, err := d.K8sClient.ListGatewayNodes()
	if err != nil {
		return errors.WithMessagef(err, "Listing the existing gatway nodes failed")
	}

	numGatewayNodes := len(gwNodes.Items)

	if numGatewayNodes == input.Gateways {
		reporter.Succeeded("Current gateways match the required number of gateways")
		return nil
	}

	// Currently, we only support increasing the number of Gateway nodes which could be a valid use-case
	// to convert a non-HA deployment to an HA deployment. We are not supporting decreasing the Gateway
	// nodes (for now) as it might impact the datapath if we accidentally delete the active GW node.
	if numGatewayNodes < input.Gateways {
		gatewayNodesToDeploy := input.Gateways - numGatewayNodes

		workerNodes, err := d.K8sClient.ListNodesWithLabel("node-role.kubernetes.io/worker")
		if err != nil {
			return errors.WithMessagef(err, "failed to list k8s nodes  in project %q", d.projectID)
		}

		nodes := workerNodes.Items
		for i := range nodes {
			alreadyTagged := nodes[i].GetLabels()[submarinerGatewayNodeTag]
			if alreadyTagged == "true" {
				continue
			}

			reporter.Started(fmt.Sprintf("Configuring worker node %q as gateway node", nodes[i].Name))

			err := d.K8sClient.AddGWLabelOnNode(nodes[i].Name)
			if err != nil {
				return errors.WithMessagef(err, "failed to label the node %q as gateway node", nodes[i].Name)
			}

			if err := d.openGatewayPort(groupName, nodes[i].Name, computeClient); err != nil {
				return errors.WithMessage(err, "failed to open the gateway port")
			}

			gatewayNodesToDeploy--
			if gatewayNodesToDeploy <= 0 {
				reporter.Succeeded("Successfully deployed gateway node")
				return nil
			}
		}

		if gatewayNodesToDeploy > 0 {
			reporter.Failed(fmt.Errorf("there are insufficient nodes to deploy the required number of gateways"))
			return nil
		}
	}

	return nil
}

func (d *ocpGatewayDeployer) Cleanup(reporter api.Reporter) error {
	reporter.Started("Removing the gateway configuration from nodes ")

	computeClient, err := openstack.NewComputeV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return errors.WithMessagef(err, "error creating the compute client for the region: %q", d.Region)
	}

	gwNodesList, err := d.K8sClient.ListGatewayNodes()
	if err != nil {
		reporter.Failed(err)
		return errors.WithMessage(err, "error listing the gateway nodes")
	}

	groupName := d.InfraID + gwSecurityGroupSuffix
	gwNodes := gwNodesList.Items

	for i := range gwNodes {
		err = d.removeGWFirewallRules(groupName, gwNodes[i].Name, computeClient)
		if err != nil {
			reporter.Failed(err)
			return errors.WithMessage(err, "error deleting the gateway secutiy group rules")
		}
	}

	err = d.K8sClient.RemoveGWLabelFromWorkerNodes()
	if err != nil {
		reporter.Failed(err)
		return errors.WithMessage(err, "failed to remove labels from worker node")
	}

	reporter.Succeeded("Successfully removed the gateway configuration from the nodes")

	reporter.Started("Retrieving the Submariner gateway firewall rules")

	err = d.deleteSG(groupName, computeClient)
	if err != nil {
		return errors.WithMessage(err, "error deleting the gateway security group")
	}

	reporter.Succeeded("Successfully deleted the g/w firewall rules")

	return nil
}

func formatPorts(ports []api.PortSpec) string {
	portStrs := []string{}
	for _, port := range ports {
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}

	return strings.Join(portStrs, ", ")
}
