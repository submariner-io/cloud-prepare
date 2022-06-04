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

package azure

import (
	"bytes"
	"context"
	"strings"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2021-12-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-03-01/network"
	"github.com/Azure/go-autorest/autorest"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/admiral/pkg/stringset"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const (
	submarinerGatewayGW      = "subgw"
	azureVirtualMachines     = "virtualMachines"
	topologyLabel            = "topology.kubernetes.io/zone"
	submarinerGatewayNodeTag = "submariner-io-gateway-node"
)

type ocpGatewayDeployer struct {
	CloudInfo
	azure           *azureCloud
	msDeployer      ocp.MachineSetDeployer
	instanceType    string
	dedicatedGWNode bool
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable deploying gateways using OCP.
// If the supplied cloud is not an azureCloud, an error is returned.
func NewOcpGatewayDeployer(info *CloudInfo, cloud api.Cloud, msDeployer ocp.MachineSetDeployer, instanceType string,
	dedicatedGWNode bool,
) (api.GatewayDeployer, error) {
	azure, ok := cloud.(*azureCloud)
	if !ok {
		return nil, errors.New("the cloud must be Azure")
	}

	return &ocpGatewayDeployer{
		CloudInfo:       *info,
		azure:           azure,
		msDeployer:      msDeployer,
		instanceType:    instanceType,
		dedicatedGWNode: dedicatedGWNode,
	}, nil
}

func (d *ocpGatewayDeployer) Deploy(input api.GatewayDeployInput, status reporter.Interface) error {
	status.Start("Deploying gateway node")

	gwNodes, err := d.azure.K8sClient.ListGatewayNodes()
	if err != nil {
		return errors.Wrap(err, "error getting the gateway node")
	}

	gatewayNodesToDeploy := input.Gateways - len(gwNodes.Items)

	if gatewayNodesToDeploy == 0 {
		status.Success("Current gateways match the required number of gateways")
		return nil
	}

	nsgClient := getNsgClient(d.SubscriptionID, d.Authorizer)
	nwClient := getInterfacesClient(d.SubscriptionID, d.Authorizer)

	if err := d.createGWSecurityGroup(d.InfraID, input.PublicPorts, nsgClient); err != nil {
		return status.Error(err, "creating gateway security group failed")
	}

	// Currently, we only support increasing the number of Gateway nodes which could be a valid use-case
	// to convert a non-HA deployment to an HA deployment. We are not supporting decreasing the Gateway
	// nodes (for now) as it might impact the datapath if we accidentally delete the active GW node.
	if gatewayNodesToDeploy < 0 {
		status.Failure("Decreasing the number of Gateway nodes is not currently supported")
		return nil
	}

	az, err := d.getAvailabilityZones(gwNodes)
	if err != nil || az.Size() == 0 {
		return status.Error(err, "error getting the availability zones for region %q", d.Region)
	}

	if d.dedicatedGWNode {
		for _, zone := range az.Elements() {
			status.Start("Deploying dedicated gateway node")

			err = d.deployGateway(zone)
			if err != nil {
				return status.Error(err, "error deploying gateway for zone %q", zone)
			}

			gatewayNodesToDeploy--
			if gatewayNodesToDeploy <= 0 {
				status.Success("Successfully deployed gateway node")
				return nil
			}
		}
	} else {
		return d.tagExistingNode(nsgClient, nwClient, gatewayNodesToDeploy, status)
	}

	if gatewayNodesToDeploy != 0 {
		return status.Error(err, "not enough zones available in the region %q to deploy required number of gateway nodes", d.Region)
	}

	status.Success("Deployed gateway node")

	return nil
}

func (d *ocpGatewayDeployer) tagExistingNode(nsgClient *network.SecurityGroupsClient, nwClient *network.InterfacesClient,
	gatewayNodesToDeploy int, status reporter.Interface,
) error {
	groupName := d.InfraID + externalSecurityGroupSuffix

	workerNodes, err := d.K8sClient.ListNodesWithLabel("node-role.kubernetes.io/worker")
	if err != nil {
		return status.Error(err, "failed to list k8s nodes in ResorceGroup %q", d.BaseGroupName)
	}

	nodes := workerNodes.Items
	for i := range nodes {
		alreadyTagged := nodes[i].GetLabels()[submarinerGatewayNodeTag]
		if alreadyTagged == "true" {
			continue
		}

		status.Start("Configuring worker node %q as Submariner gateway node", nodes[i].Name)

		err := d.K8sClient.AddGWLabelOnNode(nodes[i].Name)
		if err != nil {
			return status.Error(err, "failed to label the node %q as Submariner gateway node", nodes[i].Name)
		}

		interfaceName := nodes[i].Name + "-nic"
		if err = d.openGWSecurityGroup(interfaceName, groupName, nsgClient, nwClient); err != nil {
			return status.Error(err, "failed to open the Submariner gateway port")
		}

		gatewayNodesToDeploy--
		if gatewayNodesToDeploy <= 0 {
			status.Success("Successfully deployed Submariner gateway node")
			status.End()

			return nil
		}

		if gatewayNodesToDeploy > 0 {
			return status.Error(err, "there are insufficient nodes to deploy the required number of gateways")
		}
	}

	return nil
}

type machineSetConfig struct {
	Name         string
	AZ           string
	InfraID      string
	InstanceType string
	Region       string
}

func (d *ocpGatewayDeployer) loadGatewayYAML(name, zone string) ([]byte, error) {
	var buf bytes.Buffer

	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing machine set YAML")
	}

	tplVars := machineSetConfig{
		Name:         name,
		InfraID:      d.azure.InfraID,
		InstanceType: d.instanceType,
		Region:       d.azure.Region,
		AZ:           zone,
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, errors.Wrap(err, "error executing the template")
	}

	return buf.Bytes(), nil
}

func (d *ocpGatewayDeployer) initMachineSet(name, zone string) (*unstructured.Unstructured, error) {
	gatewayYAML, err := d.loadGatewayYAML(name, zone)
	if err != nil {
		return nil, err
	}

	unstructDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	machineSet := &unstructured.Unstructured{}

	_, _, err = unstructDecoder.Decode(gatewayYAML, nil, machineSet)
	if err != nil {
		return nil, errors.Wrap(err, "error converting YAML to machine set")
	}

	return machineSet, nil
}

func (d *ocpGatewayDeployer) deployGateway(zone string) error {
	name := d.azure.InfraID + submarinerGatewayGW + d.azure.Region + "-" + zone

	machineSet, err := d.initMachineSet(name, zone)
	if err != nil {
		return err
	}

	return errors.Wrapf(d.msDeployer.Deploy(machineSet), "error deploying machine set %q", machineSet.GetName())
}

func (d *ocpGatewayDeployer) getAvailabilityZones(gwNodesList *v1.NodeList) (stringset.Interface, error) {
	zonesWithSubmarinerGW := stringset.New()
	gwNodes := gwNodesList.Items

	for i := range gwNodes {
		zonesWithSubmarinerGW.Add(gwNodes[i].GetLabels()[topologyLabel])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	resourceSKUClient := getResourceSkuClient(d.azure.SubscriptionID, d.azure.Authorizer)

	resourceSKUs, err := resourceSKUClient.List(ctx, d.azure.Region, "")
	if err != nil {
		return nil, errors.Wrapf(err, "error getting the resource sku in the regiom %q", d.azure.Region)
	}

	eligibleZonesForSubmarinerGW := stringset.New()

	for _, resourceSKUValue := range resourceSKUs.Values() {
		if *resourceSKUValue.ResourceType == azureVirtualMachines && *resourceSKUValue.Name == d.instanceType {
			for _, zone := range *(*resourceSKUValue.LocationInfo)[0].Zones {
				if !zonesWithSubmarinerGW.Contains(d.azure.Region + "-" + zone) {
					eligibleZonesForSubmarinerGW.Add(zone)
				}
			}
		}
	}

	return eligibleZonesForSubmarinerGW, nil
}

func (d *ocpGatewayDeployer) Cleanup(status reporter.Interface) error {
	status.Start("Removing gateway node")

	nsgClient := getNsgClient(d.SubscriptionID, d.Authorizer)
	nwClient := getInterfacesClient(d.SubscriptionID, d.Authorizer)

	if err := d.removeGWSecurityGroup(d.InfraID, nsgClient, nwClient); err != nil {
		return status.Error(err, "deleting gateway security group failed")
	}

	err := d.deleteGateway()
	if err != nil {
		return status.Error(err, "removing gateway node failed")
	}

	status.Success("Removed gateway node")

	return nil
}

func (d *ocpGatewayDeployer) deleteGateway() error {
	gwNodes, err := d.azure.K8sClient.ListGatewayNodes()
	if err != nil {
		return errors.Wrapf(err, "error getting the gw nodes")
	}

	gwNodesList := gwNodes.Items

	for i := 0; i < len(gwNodesList); i++ {
		if !strings.Contains(gwNodesList[i].Name, submarinerGatewayGW) {
			err = d.K8sClient.RemoveGWLabelFromWorkerNode(&gwNodesList[i])
			if err != nil {
				return errors.Wrapf(err, "failed to remove labels from worker node")
			}
		} else {
			machineSetName := gwNodesList[i].Name[:strings.LastIndex(gwNodesList[i].Name, "-")]
			prefix := machineSetName[:strings.LastIndex(gwNodesList[i].Name, "-")]
			zone := machineSetName[strings.LastIndex(gwNodesList[i].Name, "-")-1:]

			machineSet, err := d.initMachineSet(prefix, zone)
			if err != nil {
				return err
			}

			return errors.Wrapf(d.msDeployer.Delete(machineSet), "error deleting machine set %q", machineSet.GetName())
		}
	}

	return nil
}

func getResourceSkuClient(subscriptionID string, authorizer autorest.Authorizer) *compute.ResourceSkusClient {
	resourceSkusClient := compute.NewResourceSkusClient(subscriptionID)
	resourceSkusClient.Authorizer = authorizer

	return &resourceSkusClient
}
