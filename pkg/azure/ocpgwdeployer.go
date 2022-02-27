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
	"text/template"

	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/ocp"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

type ocpGatewayDeployer struct {
	azure        *azureCloud
	msDeployer   ocp.MachineSetDeployer
	instanceType string
}

// NewOcpGatewayDeployer returns a GatewayDeployer capable deploying gateways using OCP.
// If the supplied cloud is not an azureCloud, an error is returned.
func NewOcpGatewayDeployer(cloud api.Cloud, msDeployer ocp.MachineSetDeployer, instanceType string) (api.GatewayDeployer, error) {
	azure, ok := cloud.(*azureCloud)
	if !ok {
		return nil, errors.New("the cloud must be Azure")
	}

	return &ocpGatewayDeployer{
		azure:        azure,
		msDeployer:   msDeployer,
		instanceType: instanceType,
	}, nil
}

func (d *ocpGatewayDeployer) Deploy(input api.GatewayDeployInput, reporter api.Reporter) error {
	reporter.Started("Deploying gateway node")

	err := d.deployGateway()
	if err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Deployed gateway node")

	return nil
}

type machineSetConfig struct {
	AZ           string
	InfraID      string
	InstanceType string
	Region       string
}

func (d *ocpGatewayDeployer) loadGatewayYAML() ([]byte, error) {
	var buf bytes.Buffer

	// TODO: Not working properly, but we should revisit this as it makes more sense
	// tpl, err := template.ParseFiles("pkg/aws/gw-machineset.yaml.template")
	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing machine set YAML")
	}

	tplVars := machineSetConfig{
		AZ:           "1",
		InfraID:      d.azure.InfraID,
		InstanceType: d.instanceType,
		Region:       d.azure.Region,
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, errors.Wrap(err, "error executing the template")
	}

	return buf.Bytes(), nil
}

func (d *ocpGatewayDeployer) initMachineSet() (*unstructured.Unstructured, error) {
	gatewayYAML, err := d.loadGatewayYAML()
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

func (d *ocpGatewayDeployer) deployGateway() error {
	machineSet, err := d.initMachineSet()
	if err != nil {
		return err
	}

	return errors.Wrapf(d.msDeployer.Deploy(machineSet), "error deploying machine set %q", machineSet.GetName())
}

func (d *ocpGatewayDeployer) Cleanup(reporter api.Reporter) error {
	reporter.Started("Removing gateway node")

	err := d.deleteGateway()
	if err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Removed gateway node")

	return nil
}

func (d *ocpGatewayDeployer) deleteGateway() error {
	machineSet, err := d.initMachineSet()
	if err != nil {
		return err
	}

	return errors.Wrapf(d.msDeployer.Delete(machineSet), "error deleting machine set %q", machineSet.GetName())
}
