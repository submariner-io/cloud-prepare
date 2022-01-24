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
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	gcpclient "github.com/submariner-io/cloud-prepare/pkg/gcp/client"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/dns/v1"
	"google.golang.org/api/option"
	"os"
	"strings"
)

type gcpCloud struct {
	CloudInfo
}

// NewCloud creates a new api.Cloud instance which can prepare GCP for Submariner to be deployed on it.
func NewCloud(info CloudInfo) api.Cloud {
	return &gcpCloud{CloudInfo: info}
}

// newCloudInfoFromConfig creates a new CloudInfo instance based on an AWS configuration
func newCloudInfoFromConfig(gcpClient gcpclient.Interface, projectID, infraID, region string) *CloudInfo {
	return &CloudInfo{
		ProjectID: projectID,
		InfraID:   infraID,
		Region:    region,
		Client:    gcpClient,
	}
}

// NewCloudInfoFromSettings creates a new CloudInfo instance using the given credentials file and profile
func NewCloudInfoFromSettings(credentialsFile, projectID, infraID, region string) (*CloudInfo, error) {
	creds, err := getGCPCredentials(credentialsFile)
	options := []option.ClientOption{
		option.WithCredentials(creds),
		option.WithUserAgent("open-cluster-management.io submarineraddon/v1"),
	}
	gcpClient, err := gcpclient.NewClient(projectID, options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize GCP Client")
	}

	return newCloudInfoFromConfig(gcpClient, projectID, infraID, region), nil
}

func getGCPCredentials(credentialsFile string) (*google.Credentials, error) {
	authJSON, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, errors.Wrapf(err, "error reading file %q", credentialsFile)
	}

	creds, err := google.CredentialsFromJSON(context.TODO(), authJSON, dns.CloudPlatformScope)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing credentials file")
	}

	return creds, nil
}

// PrepareForSubmariner prepares submariner cluster environment on GCP.
func (gc *gcpCloud) PrepareForSubmariner(input api.PrepareForSubmarinerInput, reporter api.Reporter) error {
	// Create the inbound firewall rule for submariner internal ports.
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

// CreateVpcPeering Creates a VPC Peering to the target cloud. Only the same
// Cloud Provider is supported
func (gc *gcpCloud) CreateVpcPeering(target api.Cloud, reporter api.Reporter) error {
	switch target.(type) {
	case *gcpCloud:
		// TODO: implement me
		return nil
	default:
		return errors.Errorf("only GCP clients are supported")
	}
}

// CleanupAfterSubmariner clean up submariner cluster environment on GCP.
func (gc *gcpCloud) CleanupAfterSubmariner(reporter api.Reporter) error {
	// Delete the inbound and outbound firewall rules to close submariner internal ports.
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
