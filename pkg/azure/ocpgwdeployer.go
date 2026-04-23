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
	"fmt"
	"strconv"
	"text/template"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v10"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/set"
)

const (
	submarinerGatewayGW  = "subgw-"
	AzureVirtualMachines = "virtualMachines"
)

type ocpGatewayDeployer struct {
	CloudInfo
	msDeployer   ocp.MachineSetDeployer
	instanceType string
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable deploying gateways using OCP.
func NewOcpGatewayDeployer(info *CloudInfo, msDeployer ocp.MachineSetDeployer, instanceType string) api.GatewayDeployer {
	return &ocpGatewayDeployer{
		CloudInfo:    *info,
		msDeployer:   msDeployer,
		instanceType: instanceType,
	}
}

func (d *ocpGatewayDeployer) Deploy(ctx context.Context, input api.GatewayDeployInput, status reporter.Interface) error {
	if input.Gateways == 0 {
		return nil
	}

	nsgClient, nwClient, pubIPClient, err := d.getClients(status)
	if err != nil {
		return err
	}

	groupName := d.InfraID + ExternalSecurityGroupSuffix

	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()

	status.Start("Retrieving gateway machine sets and nodes")

	machineSets, err := d.msDeployer.List(ctx)
	if err != nil {
		return status.Error(err, "error getting the gateway machinesets")
	}

	gwNodes, err := d.K8sClient.ListGatewayNodes(ctx)
	if err != nil {
		return status.Error(err, "error getting the gateway node")
	}

	status.End()

	gwNodeItems := gwNodes.Items
	taggedExistingNodes := ocp.RemoveDuplicates(machineSets, gwNodeItems)
	gatewayNodesToDeploy := input.Gateways - len(machineSets) - len(taggedExistingNodes)

	if len(machineSets) != 0 || gatewayNodesToDeploy != 0 || len(gwNodeItems) != 0 {
		status.Start("Creating gateway security group")

		if err := d.createGWSecurityGroup(ctx, groupName, input.PublicPorts, nsgClient); err != nil {
			return status.Error(err, "creating gateway security group failed")
		}

		status.End()
	}

	// Open the g/w ports and assign public-ip if not already done for manually tagged nodes if any
	for i := range gwNodeItems {
		status.Start("Preparing the gateway network interface for node %q", gwNodeItems[i].GetName())

		if err = d.prepareGWInterface(ctx, gwNodeItems[i].GetName(), groupName, nsgClient, nwClient, pubIPClient); err != nil {
			return status.Error(err, "failed to open the Submariner gateway port for already existing nodes")
		}

		status.End()
	}

	if gatewayNodesToDeploy == 0 {
		status.Success("Current gateways match the required number of gateways")
		return nil
	}

	// Currently, we only support increasing the number of Gateway nodes which could be a valid use-case
	// to convert a non-HA deployment to an HA deployment. We are not supporting decreasing the Gateway
	// nodes (for now) as it might impact the datapath if we accidentally delete the active GW node.
	if gatewayNodesToDeploy < 0 {
		status.Failure("Decreasing the number of Gateway nodes is not currently supported")
		return nil
	}

	image, imageErr := d.msDeployer.GetWorkerNodeImage(ctx, nil, d.InfraID)
	if imageErr != nil {
		return errors.Wrap(imageErr, "error retrieving worker node image")
	}

	return d.deployDedicatedGWNode(ctx, machineSets, gatewayNodesToDeploy, input.AirGapped, &image, status)
}

func (d *ocpGatewayDeployer) deployDedicatedGWNode(ctx context.Context, gwNodes []unstructured.Unstructured,
	gatewayNodesToDeploy int, airGapped bool, image *ocp.ImageSpec, status reporter.Interface,
) error {
	status.Start("Retrieving the availability zones for region %q ", d.Region)

	az, err := d.getAvailabilityZones(ctx, gwNodes)
	if err != nil {
		return status.Error(err, "error getting the availability zones for region %q", d.Region)
	}

	status.End()

	for _, zone := range az.UnsortedList() {
		status.Start("Deploying dedicated gateway node for zone %q", zone)

		err := d.deployGateway(ctx, zone, image, airGapped)
		if err != nil {
			return status.Error(err, "error deploying gateway for zone %q", zone)
		}

		status.End()

		gatewayNodesToDeploy--
		if gatewayNodesToDeploy <= 0 {
			break
		}
	}

	if gatewayNodesToDeploy != 0 {
		return status.Error(fmt.Errorf("not enough zones available in the region %q to deploy required number of gateway nodes",
			d.Region), "")
	}

	return nil
}

type machineSetConfig struct {
	Name            string
	AZ              string
	InfraID         string
	InstanceType    string
	Region          string
	ImageResourceID string
	ImageOffer      string
	ImagePublisher  string
	ImageSKU        string
	ImageVersion    string
	ImageType       string
	PublicIP        string
}

func (d *ocpGatewayDeployer) loadGatewayYAML(name, zone string, image *ocp.ImageSpec, airGapped bool) ([]byte, error) {
	var buf bytes.Buffer

	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing machine set YAML")
	}

	tplVars := machineSetConfig{
		Name:            name,
		InfraID:         d.InfraID,
		InstanceType:    d.instanceType,
		Region:          d.Region,
		AZ:              zone,
		ImageResourceID: image.ResourceID,
		ImageOffer:      image.Offer,
		ImagePublisher:  image.Publisher,
		ImageSKU:        image.SKU,
		ImageVersion:    image.Version,
		ImageType:       image.Type,
		PublicIP:        strconv.FormatBool(!airGapped),
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, errors.Wrap(err, "error executing the template")
	}

	return buf.Bytes(), nil
}

func (d *ocpGatewayDeployer) initMachineSet(name, zone string, image *ocp.ImageSpec, airGapped bool) (*unstructured.Unstructured, error) {
	gatewayYAML, err := d.loadGatewayYAML(name, zone, image, airGapped)
	if err != nil {
		return nil, err
	}

	unStructDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	machineSet := &unstructured.Unstructured{}

	_, _, err = unStructDecoder.Decode(gatewayYAML, nil, machineSet)
	if err != nil {
		return nil, errors.Wrap(err, "error converting YAML to machine set")
	}

	return machineSet, nil
}

func (d *ocpGatewayDeployer) deployGateway(ctx context.Context, zone string, image *ocp.ImageSpec, airGapped bool) error {
	machineSet, err := d.initMachineSet(MachineName(d.Region), zone, image, airGapped)
	if err != nil {
		return err
	}

	return errors.Wrapf(d.msDeployer.Deploy(ctx, machineSet), "error deploying machine set %q", machineSet.GetName())
}

// MachineName generates a machine name for the gateway.
// The name length is limited to 20 characters to ensure we don't hit the 63-character limit
// when generating the "machine public IP name".
// At most 7 characters for the region,
// at most 13 for the region and a randomly generated UUID.
// We add "subgw-", 6 characters, for a total of 20 with the hyphen between region and UUID.
func MachineName(region string) string {
	if len(region) > 7 {
		region = region[0:7]
	}

	return submarinerGatewayGW + region + "-" + string(uuid.NewUUID())[0:6]
}

func (d *ocpGatewayDeployer) getAvailabilityZones(ctx context.Context, gwNodes []unstructured.Unstructured,
) (set.Set[string], error) {
	zonesWithSubmarinerGW := set.New[string]()

	for i := range gwNodes {
		zone, _, err := unstructured.NestedString(gwNodes[i].Object, "spec", "template", "spec", "providerSpec", "value", "zone")
		if err != nil {
			return nil, errors.Wrap(err, "error getting the zone from the existing node")
		}

		zonesWithSubmarinerGW.Insert(zone)
	}

	resourceSKUClient, err := d.getResourceSKUClient()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to get resource SKU client")
	}

	pager := resourceSKUClient.NewListPager(&armcompute.ResourceSKUsClientListOptions{
		Filter: new(d.Region),
	})

	eligibleZonesForSubmarinerGW := set.New[string]()

	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "error paging the resource SKUs in the region %q", d.Region)
		}

		for _, resourceSKU := range nextResult.Value {
			zones := d.filterZonesByRegion(resourceSKU, zonesWithSubmarinerGW)
			eligibleZonesForSubmarinerGW = eligibleZonesForSubmarinerGW.Union(zones)
		}
	}

	return eligibleZonesForSubmarinerGW, nil
}

func (d *ocpGatewayDeployer) filterZonesByRegion(resourceSKU *armcompute.ResourceSKU, zonesWithSubmarinerGW set.Set[string],
) set.Set[string] {
	eligibleZones := set.New[string]()

	if resourceSKU == nil || resourceSKU.ResourceType == nil || resourceSKU.Name == nil {
		return eligibleZones
	}

	if *resourceSKU.ResourceType != AzureVirtualMachines || *resourceSKU.Name != d.instanceType {
		return eligibleZones
	}

	for _, locationInfo := range resourceSKU.LocationInfo {
		if locationInfo == nil || locationInfo.Location == nil || *locationInfo.Location != d.Region {
			continue
		}

		for _, zone := range locationInfo.Zones {
			if zone == nil {
				continue
			}

			if !zonesWithSubmarinerGW.Has(*zone) {
				eligibleZones.Insert(*zone)
			}
		}
	}

	return eligibleZones
}

func (d *ocpGatewayDeployer) Cleanup(ctx context.Context, status reporter.Interface) error {
	status.Start("Removing gateway node")

	nsgClient, err := d.getNsgClient()
	if err != nil {
		return status.Error(err, "Failed to get network security groups client")
	}

	nwClient, err := d.getInterfacesClient()
	if err != nil {
		return status.Error(err, "Failed to get network interfaces client")
	}

	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()

	if err := d.cleanupGWInterface(ctx, d.InfraID, nsgClient, nwClient); err != nil {
		return status.Error(err, "deleting gateway security group failed")
	}

	err = d.deleteGateway(ctx, status)
	if err != nil {
		return err
	}

	status.Success("Removed gateway node")

	return nil
}

func (d *ocpGatewayDeployer) deleteGateway(ctx context.Context, status reporter.Interface) error {
	machineSetList, err := d.msDeployer.List(ctx)
	if err != nil {
		return status.Error(err, "error listing the Submariner gateway nodes")
	}

	pubIPClient, err := d.getPublicIPClient()
	if err != nil {
		return errors.Wrapf(err, "Failed to get network public IP addresses client")
	}

	for i := range machineSetList {
		status.Start("Deleting the gateway instance %q", machineSetList[i].GetName())

		err = d.msDeployer.DeleteByName(ctx, machineSetList[i].GetName(), machineSetList[i].GetNamespace())
		if err != nil {
			return status.Error(err, "error deleting the gateway instance from node: %q",
				machineSetList[i].GetName())
		}

		publicIPName := machineSetList[i].GetName() + publicIPNameSuffix

		err = d.deletePublicIP(ctx, pubIPClient, publicIPName)
		if err != nil {
			return status.Error(err, "failed to delete public-ip %q", publicIPName)
		}

		status.Success("Successfully deleted the instance")
	}

	// Cleanup nodes that are not dedicated gateway nodes.
	gwNodesList, err := d.K8sClient.ListGatewayNodes(ctx)
	if err != nil {
		return status.Error(err, "error listing the Submariner gateway nodes")
	}

	gwNodes := ocp.RemoveDuplicates(machineSetList, gwNodesList.Items)

	for i := range gwNodes {
		err = d.K8sClient.RemoveGWLabelFromWorkerNode(ctx, &gwNodes[i])
		if err != nil {
			return status.Error(err, "failed to cleanup node %q", gwNodes[i].Name)
		}

		publicIPName := gwNodes[i].Name + publicIPNameSuffix

		err = d.deletePublicIP(ctx, pubIPClient, publicIPName)
		if err != nil {
			return status.Error(err, "failed to delete public-ip")
		}
	}

	return nil
}

func (d *ocpGatewayDeployer) getClients(status reporter.Interface) (
	*armnetwork.SecurityGroupsClient, *armnetwork.InterfacesClient, *armnetwork.PublicIPAddressesClient, error,
) {
	nsgClient, err := d.getNsgClient()
	if err != nil {
		return nil, nil, nil, status.Error(err, "Failed to get network security groups client")
	}

	nwClient, err := d.getInterfacesClient()
	if err != nil {
		return nil, nil, nil, status.Error(err, "Failed to get network interfaces client")
	}

	pubIPClient, err := d.getPublicIPClient()
	if err != nil {
		return nil, nil, nil, status.Error(err, "Failed to get network public IP addresses client")
	}

	return nsgClient, nwClient, pubIPClient, nil
}
