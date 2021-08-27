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
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/stringset"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

type ocpGatewayDeployer struct {
	gcp          *gcpCloud
	msDeployer   ocp.MachineSetDeployer
	instanceType string
	image        string
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable deploying gateways using OCP
// If the supplied cloud is not a gcpCloud, an error is returned
func NewOcpGatewayDeployer(cloud api.Cloud, msDeployer ocp.MachineSetDeployer, instanceType, image string) (api.GatewayDeployer, error) {
	gcp, ok := cloud.(*gcpCloud)
	if !ok {
		return nil, errors.New("the cloud must be GCP")
	}

	return &ocpGatewayDeployer{
		gcp:          gcp,
		msDeployer:   msDeployer,
		instanceType: instanceType,
		image:        image,
	}, nil
}

func (d *ocpGatewayDeployer) Deploy(input api.GatewayDeployInput, reporter api.Reporter) error {
	reporter.Started(messageCreateExtFWRules)

	externalIngress := newExternalFirewallRules(d.gcp.projectID, d.gcp.infraID, input.PublicPorts)
	if err := d.gcp.openPorts(externalIngress); err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Opened External ports %q with firewall rule %q on GCP",
		formatPorts(input.PublicPorts), externalIngress.Name)

	reporter.Started(messageRetrieveZones)

	zones, err := d.gcp.client.ListZones()
	if err != nil {
		return reportFailure(reporter, err, "failed to list the zones in the project %q. %v", d.gcp.projectID)
	}

	reporter.Succeeded(messageRetrievedZones)
	reporter.Started(messageValidateCurrentGWCount)

	zonesWithSubmarinerGW := stringset.New()
	eligibleZonesForGW := stringset.New()

	for _, zone := range zones.Items {
		if d.ignoreZone(zone) {
			continue
		}

		instanceList, err := d.gcp.client.ListInstances(zone.Name)
		if err != nil {
			return reportFailure(reporter, err, "failed to list instances in zone %q of project %q", zone.Name, d.gcp.projectID)
		}

		for _, instance := range instanceList.Items {
			prefix := d.gcp.infraID + "-submariner-gw-" + zone.Name
			if strings.HasPrefix(instance.Name, prefix) {
				zonesWithSubmarinerGW.Add(zone.Name)
				break
			}
		}

		if !zonesWithSubmarinerGW.Contains(zone.Name) {
			eligibleZonesForGW.Add(zone.Name)
		}
	}

	if zonesWithSubmarinerGW.Size() == input.Gateways {
		reporter.Succeeded(messageValidatedCurrentGWs)
		return nil
	}

	if zonesWithSubmarinerGW.Size() < input.Gateways {
		gatewayNodesToDeploy := input.Gateways - zonesWithSubmarinerGW.Size()

		reporter.Started(messageDeployGatewayNode)

		for _, zone := range eligibleZonesForGW.Elements() {
			err := d.deployGateway(zone)
			if err != nil {
				reporter.Failed(err)
				return err
			}

			gatewayNodesToDeploy--
			if gatewayNodesToDeploy <= 0 {
				reporter.Succeeded(messageDeployedGatewayNode)
				return nil
			}
		}

		if gatewayNodesToDeploy > 0 {
			reporter.Failed(fmt.Errorf(messageInsufficientZonesForDeploy))
			return nil
		}
	}

	return nil
}

type machineSetConfig struct {
	AZ                  string
	InfraID             string
	ProjectID           string
	InstanceType        string
	Region              string
	Image               string
	SubmarinerGWNodeTag string
}

func (d *ocpGatewayDeployer) loadGatewayYAML(zone, image string) ([]byte, error) {
	var buf bytes.Buffer

	// TODO: Not working properly, but we should revisit this as it makes more sense
	// tpl, err := template.ParseFiles("pkg/aws/gw-machineset.yaml")
	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, err
	}

	tplVars := machineSetConfig{
		AZ:                  zone,
		InfraID:             d.gcp.infraID,
		ProjectID:           d.gcp.projectID,
		InstanceType:        d.instanceType,
		Region:              d.gcp.region,
		Image:               image,
		SubmarinerGWNodeTag: submarinerGatewayNodeTag,
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (d *ocpGatewayDeployer) initMachineSet(zone string) (*unstructured.Unstructured, error) {
	gatewayYAML, err := d.loadGatewayYAML(zone, d.image)
	if err != nil {
		return nil, err
	}

	unstructDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	machineSet := &unstructured.Unstructured{}
	_, _, err = unstructDecoder.Decode(gatewayYAML, nil, machineSet)
	if err != nil {
		return nil, err
	}

	return machineSet, nil
}

func (d *ocpGatewayDeployer) deployGateway(zone string) error {
	machineSet, err := d.initMachineSet(zone)
	if err != nil {
		return err
	}

	if d.image == "" {
		d.image, err = d.msDeployer.GetWorkerNodeImage(machineSet, d.gcp.infraID)
		if err != nil {
			return err
		}

		machineSet, err = d.initMachineSet(zone)
		if err != nil {
			return err
		}
	}

	return d.msDeployer.Deploy(machineSet)
}

func (d *ocpGatewayDeployer) Cleanup(reporter api.Reporter) error {
	reporter.Started(messageDeleteExtFWRules)
	err := d.deleteExternalFWRules(reporter)
	if err != nil {
		return reportFailure(reporter, err, "failed to delete the gateway firewall rules in the project %q", d.gcp.projectID)
	}

	reporter.Succeeded(messageDeletedExtFWRules)
	reporter.Started(messageRetrieveZones)

	zones, err := d.gcp.client.ListZones()
	if err != nil {
		return reportFailure(reporter, err, "failed to list the zones in the project %q", d.gcp.projectID)
	}

	reporter.Succeeded(messageRetrievedZones)
	reporter.Started(messageVerifyCurrentGWCount)

	for _, zone := range zones.Items {
		if d.ignoreZone(zone) {
			continue
		}

		instanceList, err := d.gcp.client.ListInstances(zone.Name)
		if err != nil {
			return reportFailure(reporter, err, "failed to list instances in zone %q of project %q", zone.Name, d.gcp.projectID)
		}

		for _, instance := range instanceList.Items {
			prefix := d.gcp.infraID + "-submariner-gw-" + zone.Name
			if strings.HasPrefix(instance.Name, prefix) {
				err := d.deleteGateway(zone.Name)
				if err != nil {
					return reportFailure(reporter, err, "failed to delete gateway instance %q", instance.Name)
				}
			}
		}
	}

	reporter.Succeeded(messageVerifiedCurrentGWCount)

	return nil
}

func (d *ocpGatewayDeployer) deleteGateway(zone string) error {
	machineSet, err := d.initMachineSet(zone)
	if err != nil {
		return err
	}

	return d.msDeployer.Delete(machineSet)
}

func (d *ocpGatewayDeployer) deleteExternalFWRules(reporter api.Reporter) error {
	ingressName := generateRuleName(d.gcp.infraID, publicPortsRuleName)

	if err := d.gcp.deleteFirewallRule(ingressName, reporter); err != nil {
		reporter.Failed(err)
		return err
	}

	return nil
}

func reportFailure(reporter api.Reporter, failure error, format string, args ...string) error {
	err := errors.WithMessagef(failure, format, args)
	reporter.Failed(err)

	return err
}

func (d *ocpGatewayDeployer) ignoreZone(zone *compute.Zone) bool {
	region := zone.Region[strings.LastIndex(zone.Region, "/")+1:]

	return region != d.gcp.region
}
