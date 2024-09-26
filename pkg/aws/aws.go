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

package aws

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	awsClient "github.com/submariner-io/cloud-prepare/pkg/aws/client"
)

type CloudOption func(*awsCloud)

const (
	ControlPlaneSecurityGroupIDKey = "controlPlaneSecurityGroupID"
	WorkerSecurityGroupIDKey       = "workerSecurityGroupID"
	PublicSubnetListKey            = "PublicSubnetList"
	VPCIDKey                       = "VPCID"
)

func WithControlPlaneSecurityGroup(id string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.cloudConfig[ControlPlaneSecurityGroupIDKey] = id
	}
}

func WithWorkerSecurityGroup(id string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.cloudConfig[WorkerSecurityGroupIDKey] = id
	}
}

func WithPublicSubnetList(id []string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.cloudConfig[PublicSubnetListKey] = id
	}
}

func WithVPCName(name string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.cloudConfig[VPCIDKey] = name
	}
}

const (
	messageRetrieveVPCID          = "Retrieving VPC ID"
	messageRetrievedVPCID         = "Retrieved VPC ID %s"
	messageValidatePrerequisites  = "Validating pre-requisites"
	messageValidatedPrerequisites = "Validated pre-requisites"
)

type awsCloud struct {
	client               awsClient.Interface
	infraID              string
	region               string
	nodeSGSuffix         string
	controlPlaneSGSuffix string
	cloudConfig          map[string]interface{}
}

// NewCloud creates a new api.Cloud instance which can prepare AWS for Submariner to be deployed on it.
func NewCloud(client awsClient.Interface, infraID, region string, opts ...CloudOption) api.Cloud {
	cloud := &awsCloud{
		client:      client,
		infraID:     infraID,
		region:      region,
		cloudConfig: make(map[string]interface{}),
	}

	for _, opt := range opts {
		opt(cloud)
	}

	return cloud
}

// NewCloudFromConfig creates a new api.Cloud instance based on an AWS configuration
// which can prepare AWS for Submariner to be deployed on it.
func NewCloudFromConfig(cfg *aws.Config, infraID, region string, opts ...CloudOption) api.Cloud {
	cloud := &awsCloud{
		client:      ec2.NewFromConfig(*cfg),
		infraID:     infraID,
		region:      region,
		cloudConfig: make(map[string]interface{}),
	}

	for _, opt := range opts {
		opt(cloud)
	}

	return cloud
}

// NewCloudFromSettings creates a new api.Cloud instance using the given credentials file and profile
// which can prepare AWS for Submariner to be deployed on it.
func NewCloudFromSettings(credentialsFile, profile, infraID, region string, opts ...CloudOption) (api.Cloud, error) {
	options := []func(*config.LoadOptions) error{config.WithRegion(region), config.WithSharedConfigProfile(profile)}
	if credentialsFile != DefaultCredentialsFile() {
		options = append(options, config.WithSharedCredentialsFiles([]string{credentialsFile}))
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), options...)
	if err != nil {
		return nil, errors.Wrap(err, "error loading default config")
	}

	return NewCloudFromConfig(&cfg, infraID, region, opts...), nil
}

// DefaultCredentialsFile returns the default credentials file name.
func DefaultCredentialsFile() string {
	return config.DefaultSharedCredentialsFilename()
}

// DefaultProfile returns the default profile name.
func DefaultProfile() string {
	return "default"
}

func (ac *awsCloud) setSuffixes(vpcID string) error {
	if ac.nodeSGSuffix != "" {
		return nil
	}

	var publicSubnets []types.Subnet

	if subnets, exists := ac.cloudConfig[PublicSubnetListKey]; exists {
		if subnetIDs, ok := subnets.([]string); ok && len(subnetIDs) > 0 {
			for _, id := range subnetIDs {
				subnet, err := ac.getSubnetByID(id)
				if err != nil {
					return errors.Wrapf(err, "unable to find subnet with ID %s", id)
				}

				publicSubnets = append(publicSubnets, *subnet)
			}
		} else {
			return errors.New("Subnet IDs must be a valid non-empty slice of strings")
		}
	} else {
		publicSubnets, err := ac.findPublicSubnets(vpcID, ac.filterByName("{infraID}*-public-{region}*"))
		if err != nil {
			return errors.Wrapf(err, "unable to find the public subnet")
		}

		if len(publicSubnets) == 0 {
			return errors.New("no public subnet found")
		}
	}

	pattern := fmt.Sprintf(`%s.*-subnet-public-%s.*`, regexp.QuoteMeta(ac.infraID), regexp.QuoteMeta(ac.region))
	re := regexp.MustCompile(pattern)

	for i := range publicSubnets {
		tags := publicSubnets[i].Tags
		for i := range tags {
			if strings.Contains(*tags[i].Key, "Name") && re.MatchString(*tags[i].Value) {
				ac.nodeSGSuffix = "-node"
				ac.controlPlaneSGSuffix = "-controlplane"

				return nil
			}
		}
	}

	ac.nodeSGSuffix = "-worker-sg"
	ac.controlPlaneSGSuffix = "-master-sg"

	return nil
}

func (ac *awsCloud) OpenPorts(ports []api.PortSpec, status reporter.Interface) error {
	status.Start(messageRetrieveVPCID)
	defer status.End()

	vpcID, err := ac.getVpcID()
	if err != nil {
		return status.Error(err, "unable to retrieve the VPC ID")
	}

	if _, found := ac.cloudConfig[VPCIDKey]; !found {
		err = ac.setSuffixes(vpcID)
		if err != nil {
			return status.Error(err, "unable to retrieve the security group names")
		}
	}

	status.Success(messageRetrievedVPCID, vpcID)

	status.Start(messageValidatePrerequisites)

	err = ac.validatePreparePrerequisites(vpcID)
	if err != nil {
		return status.Error(err, "unable to validate prerequisites")
	}

	status.Success(messageValidatedPrerequisites)

	for _, port := range ports {
		status.Start("Opening port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)

		err = ac.allowPortInCluster(vpcID, port.Port, port.Protocol)
		if err != nil {
			return status.Error(err, "unable to open port")
		}

		status.Success("Opened port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)
	}

	return nil
}

func (ac *awsCloud) validatePreparePrerequisites(vpcID string) error {
	return ac.validateCreateSecGroupRule(vpcID)
}

func (ac *awsCloud) ClosePorts(status reporter.Interface) error {
	status.Start(messageRetrieveVPCID)
	defer status.End()

	vpcID, err := ac.getVpcID()
	if err != nil {
		return status.Error(err, "unable to retrieve the VPC ID")
	}

	if _, found := ac.cloudConfig[VPCIDKey]; !found {
		err = ac.setSuffixes(vpcID)
		if err != nil {
			return status.Error(err, "unable to retrieve the security group names")
		}
	}

	status.Success(messageRetrievedVPCID, vpcID)

	status.Start(messageValidatePrerequisites)

	err = ac.validateCleanupPrerequisites(vpcID)
	if err != nil {
		return status.Error(err, "unable to validate prerequisites")
	}

	status.Success(messageValidatedPrerequisites)

	status.Start("Revoking intra-cluster communication permissions")

	err = ac.revokePortsInCluster(vpcID)
	if err != nil {
		return status.Error(err, "unable to revoke permissions")
	}

	status.Success("Revoked intra-cluster communication permissions")

	return nil
}

func (ac *awsCloud) validateCleanupPrerequisites(vpcID string) error {
	return ac.validateDeleteSecGroupRule(vpcID)
}
