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

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/reporter"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	awsClient "github.com/submariner-io/cloud-prepare/pkg/aws/client"
)

const (
	messageRetrieveVPCID          = "Retrieving VPC ID"
	messageRetrievedVPCID         = "Retrieved VPC ID %s"
	messageValidatePrerequisites  = "Validating pre-requisites"
	messageValidatedPrerequisites = "Validated pre-requisites"
)

type CloudOption func(*awsCloud)

func WithControlPlaneSecurityGroup(id string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.controlPlaneGroupID = id
	}
}

func WithWorkerSecurityGroup(id string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.workerGroupID = id
	}
}

func WithPublicSubnetList(s []string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.publicSubnetList = s
	}
}

func WithVPCName(name string) CloudOption {
	return func(cloud *awsCloud) {
		cloud.vpcID = name
	}
}

type awsCloud struct {
	client               awsClient.Interface
	infraID              string
	region               string
	nodeSGSuffix         string
	controlPlaneSGSuffix string
	controlPlaneGroupID  string
	workerGroupID        string
	publicSubnetList     []string
	vpcID                string
}

// NewCloud creates a new api.Cloud instance which can prepare AWS for Submariner to be deployed on it.
func NewCloud(client awsClient.Interface, infraID, region string, opts ...CloudOption) api.Cloud {
	cloud := &awsCloud{
		client:  client,
		infraID: infraID,
		region:  region,
	}

	for _, opt := range opts {
		opt(cloud)
	}

	return cloud
}

func (ac *awsCloud) setSuffixes(ctx context.Context, vpcID string) error {
	if ac.nodeSGSuffix != "" || ac.vpcID != "" {
		return nil
	}

	publicSubnets, err := ac.findPublicSubnets(ctx, vpcID, ac.publicFilter())
	if err != nil {
		return err
	}

	if len(publicSubnets) == 0 {
		return errors.New("unable to determine security group suffixes - no public subnet found")
	}

	pattern := fmt.Sprintf(`%s.*-subnet-public-%s.*`, regexp.QuoteMeta(ac.infraID), regexp.QuoteMeta(ac.region))
	re := regexp.MustCompile(pattern)

	for i := range publicSubnets {
		tags := publicSubnets[i].Tags
		for j := range tags {
			if strings.Contains(*tags[j].Key, "Name") && re.MatchString(*tags[j].Value) {
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

func (ac *awsCloud) OpenPorts(ctx context.Context, ports []api.PortSpec, status reporter.Interface) error {
	status.Start(messageRetrieveVPCID)
	defer status.End()

	vpcID, err := ac.getVpcID(ctx)
	if err != nil {
		return status.Error(err, "unable to retrieve the VPC ID")
	}

	err = ac.setSuffixes(ctx, vpcID)
	if err != nil {
		return status.Error(err, "")
	}

	status.Success(messageRetrievedVPCID, vpcID)

	status.Start(messageValidatePrerequisites)

	err = ac.validatePreparePrerequisites(ctx, vpcID)
	if err != nil {
		return status.Error(err, "unable to validate prerequisites")
	}

	status.Success(messageValidatedPrerequisites)

	for _, port := range ports {
		status.Start("Opening port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)

		err = ac.allowPortInCluster(ctx, vpcID, port.Port, port.Protocol)
		if err != nil {
			return status.Error(err, "unable to open port")
		}

		status.Success("Opened port %v protocol %s for intra-cluster communications", port.Port, port.Protocol)
	}

	return nil
}

func (ac *awsCloud) validatePreparePrerequisites(ctx context.Context, vpcID string) error {
	return ac.validateCreateSecGroupRule(ctx, vpcID)
}

func (ac *awsCloud) ClosePorts(ctx context.Context, status reporter.Interface) error {
	status.Start(messageRetrieveVPCID)
	defer status.End()

	vpcID, err := ac.getVpcID(ctx)
	if err != nil {
		return status.Error(err, "unable to retrieve the VPC ID")
	}

	err = ac.setSuffixes(ctx, vpcID)
	if err != nil {
		return status.Error(err, "")
	}

	status.Success(messageRetrievedVPCID, vpcID)

	status.Start(messageValidatePrerequisites)

	err = ac.validateCleanupPrerequisites(ctx, vpcID)
	if err != nil {
		return status.Error(err, "unable to validate prerequisites")
	}

	status.Success(messageValidatedPrerequisites)

	status.Start("Revoking intra-cluster communication permissions")

	err = ac.revokePortsInCluster(ctx, vpcID)
	if err != nil {
		return status.Error(err, "unable to revoke permissions")
	}

	status.Success("Revoked intra-cluster communication permissions")

	return nil
}

func (ac *awsCloud) validateCleanupPrerequisites(ctx context.Context, vpcID string) error {
	return ac.validateDeleteSecGroupRule(ctx, vpcID)
}
