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
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

type ocpGatewayDeployer struct {
	CloudInfo
	projectID    string
	instanceType string
	image        string
	cloudName    string
	msDeployer   ocp.MachineSetDeployer
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable of deploying gateways using OCP.
func NewOcpGatewayDeployer(info CloudInfo, msDeployer ocp.MachineSetDeployer, projectID, instanceType, image, cloudName string,
) api.GatewayDeployer {
	return &ocpGatewayDeployer{
		CloudInfo:    info,
		projectID:    projectID,
		instanceType: instanceType,
		image:        image,
		cloudName:    cloudName,
		msDeployer:   msDeployer,
	}
}

type machineSetConfig struct {
	Index               string
	InfraID             string
	ProjectID           string
	InstanceType        string
	Region              string
	Image               string
	SubmarinerGWNodeTag string
	CloudName           string
}

func (d *ocpGatewayDeployer) loadGatewayYAML(index, image string) ([]byte, error) {
	var buf bytes.Buffer

	// TODO: Not working properly, but we should revisit this as it makes more sense
	// tpl, err := template.ParseFiles("pkg/aws/gw-machineset.yaml.template")
	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create machine set template")
	}

	tplVars := machineSetConfig{
		Index:               index,
		InfraID:             d.InfraID,
		ProjectID:           d.projectID,
		InstanceType:        d.instanceType,
		Region:              d.Region,
		CloudName:           d.cloudName,
		Image:               image,
		SubmarinerGWNodeTag: submarinerGatewayNodeTag,
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute machine set template")
	}

	return buf.Bytes(), nil
}

func (d *ocpGatewayDeployer) initMachineSet(index string) (*unstructured.Unstructured, error) {
	gatewayYAML, err := d.loadGatewayYAML(index, d.image)
	if err != nil {
		return nil, err
	}

	unstructDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	machineSet := &unstructured.Unstructured{}
	_, _, err = unstructDecoder.Decode(gatewayYAML, nil, machineSet)

	return machineSet, errors.Wrap(err, "error decoding message gateway yaml")
}

func (d *ocpGatewayDeployer) deployGateway(index string) error {
	machineSet, err := d.initMachineSet(index)
	if err != nil {
		return err
	}

	if d.image == "" {
		d.image, err = d.msDeployer.GetWorkerNodeImage(machineSet, d.InfraID)
		if err != nil {
			return errors.Wrap(err, "error getting the worker image")
		}

		machineSet, err = d.initMachineSet(index)
		if err != nil {
			return err
		}
	}

	return errors.Wrap(d.msDeployer.Deploy(machineSet), "failed to deploy submariner gateway node")
}

func (d *ocpGatewayDeployer) Deploy(input api.GatewayDeployInput, status reporter.Interface) error {
	status.Start("Configuring the required firewall rules for inter-cluster traffic")
	defer status.End()

	computeClient, err := openstack.NewComputeV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return status.Error(err, "error creating the compute client")
	}

	networkClient, err := openstack.NewNetworkV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return status.Error(err, "error creating the network client")
	}

	groupName := d.InfraID + gwSecurityGroupSuffix
	if err := d.createGWSecurityGroup(input.PublicPorts, groupName, computeClient, networkClient); err != nil {
		return status.Error(err, "creating gateway security group failed")
	}

	machineSets, err := d.msDeployer.List()
	if err != nil {
		return status.Error(err, "error getting the gateway machinesets")
	}

	status.Success("Opened external ports %q in security group %q on RHOS for gateway nodes",
		formatPorts(input.PublicPorts), groupName)

	gatewayNodesToDeploy := input.Gateways - len(machineSets)

	if gatewayNodesToDeploy == 0 {
		status.Success("Current Submariner gateways match the required number of Submariner gateways")
		return nil
	}

	return d.deployGWNodes(input.Gateways, len(machineSets), status)
}

func (d *ocpGatewayDeployer) deployGWNodes(gatewayCount int,
	numGatewayNodes int, status reporter.Interface,
) error {
	// Currently, we only support increasing the number of Gateway nodes which could be a valid use-case
	// to convert a non-HA deployment to an HA deployment. We are not supporting decreasing the Gateway
	// nodes (for now) as it might impact the datapath if we accidentally delete the active GW node.
	if numGatewayNodes > gatewayCount {
		return nil
	}

	gatewayNodesToDeploy := gatewayCount - numGatewayNodes

	for i := 0; i < gatewayNodesToDeploy; i++ {
		gwNodeName := d.InfraID + "-submariner-gw" + strconv.Itoa(i)
		status.Start("Deploying dedicated Submariner gateway node %s", gwNodeName)

		err := d.deployGateway(strconv.Itoa(i))
		if err != nil {
			return status.Error(err, "unable to deploy gateway")
		}

		status.Success("Successfully deployed Submariner gateway node")
		status.End()
	}

	return nil
}

func (d *ocpGatewayDeployer) Cleanup(status reporter.Interface) error {
	computeClient, err := openstack.NewComputeV2(d.Client, gophercloud.EndpointOpts{Region: d.Region})
	if err != nil {
		return status.Error(err, "error creating the compute client for the region: %q", d.Region)
	}

	groupName := d.InfraID + gwSecurityGroupSuffix

	machineSetList, err := d.msDeployer.List()
	if err != nil {
		return status.Error(err, "error listing the Submariner gateway nodes")
	}

	// cleaning up the dedicated g/w nodes
	for i := range machineSetList {
		status.Start("Removing the Submariner gateway security group rules from node %q",
			machineSetList[i].GetName())

		err = d.removeFirewallRulesFromGW(groupName, machineSetList[i].GetName(), computeClient)
		if err != nil {
			return status.Error(err, "error deleting the security group rules")
		}

		status.Success("Successfully removed security group rules from node %q",
			machineSetList[i].GetName())

		status.Start(fmt.Sprintf("Deleting the gateway instance %q", machineSetList[i].GetName()))

		err = d.msDeployer.DeleteByName(machineSetList[i].GetName(), machineSetList[i].GetNamespace())
		if err != nil {
			return status.Error(err, "error deleting the gateway instance from node: %q",
				machineSetList[i].GetName())
		}

		status.Success("Successfully deleted the instance")
	}

	status.Success("Successfully cleaned up Submariner gateway nodes")

	status.Start("Deleting the Submariner gateway security group")

	err = d.deleteSG(groupName, computeClient)
	if err != nil {
		return errors.Wrap(err, "error deleting the Submariner gateway security group")
	}

	status.Success("Successfully deleted the Submariner gateway security group")
	status.End()

	return nil
}

func formatPorts(ports []api.PortSpec) string {
	portStrs := []string{}
	for _, port := range ports {
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}

	return strings.Join(portStrs, ", ")
}
