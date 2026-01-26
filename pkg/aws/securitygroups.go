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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

const internalTraffic = "Internal Submariner traffic"

func (ac *awsCloud) getSecurityGroupName(ctx context.Context, vpcID, name string) (*string, error) {
	group, err := ac.getSecurityGroup(ctx, vpcID, name)
	if err != nil {
		return nil, err
	}

	return group.GroupId, nil
}

func (ac *awsCloud) getSecurityGroupByID(ctx context.Context, groupID string) (types.SecurityGroup, error) {
	output, err := ac.client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{groupID},
	})
	if err != nil {
		return types.SecurityGroup{}, errors.Wrapf(err, "unable to describe security group %s", groupID)
	}

	if len(output.SecurityGroups) == 0 {
		return types.SecurityGroup{}, errors.New("security group not found")
	}

	return output.SecurityGroups[0], nil
}

func (ac *awsCloud) getSecurityGroup(ctx context.Context, vpcID, name string) (types.SecurityGroup, error) {
	filters := []types.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByName(name),
	}

	result, err := ac.client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: filters,
	})
	if err != nil {
		return types.SecurityGroup{}, errors.Wrap(err, "error describing AWS security groups")
	}

	if len(result.SecurityGroups) == 0 {
		return types.SecurityGroup{}, newNotFoundError("security group %s", name)
	}

	return result.SecurityGroups[0], nil
}

func (ac *awsCloud) authorizeSecurityGroupIngress(ctx context.Context, groupID *string, ipPermissions []types.IpPermission) error {
	input := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       groupID,
		IpPermissions: ipPermissions,
	}

	_, err := ac.client.AuthorizeSecurityGroupIngress(ctx, input)
	if isAWSError(err, "InvalidPermission.Duplicate") {
		return nil
	}

	return errors.Wrap(err, "error authorizing AWS security groups ingress")
}

func (ac *awsCloud) createClusterSGRule(ctx context.Context, srcGroup, destGroup *string, port uint16, protocol, description string) error {
	ipPermissions := []types.IpPermission{
		{
			FromPort:   ptr.To(int32(port)),
			ToPort:     ptr.To(int32(port)),
			IpProtocol: ptr.To(protocol),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					Description: ptr.To(description),
					GroupId:     srcGroup,
				},
			},
		},
	}

	return ac.authorizeSecurityGroupIngress(ctx, destGroup, ipPermissions)
}

func (ac *awsCloud) allowPortInCluster(ctx context.Context, vpcID string, port uint16, protocol string) error {
	var workerGroupID, controlPlaneGroupID *string
	var err error

	if id, exists := ac.cloudConfig[WorkerSecurityGroupIDKey]; exists {
		if workerGroupIDStr, ok := id.(string); ok && workerGroupIDStr != "" {
			workerGroupID = &workerGroupIDStr
		} else {
			return errors.New("Worker Security Group ID must be a valid non-empty string")
		}
	} else {
		workerGroupName := withInfraIDPrefix(ac.nodeSGSuffix)

		workerGroupID, err = ac.getSecurityGroupName(ctx, vpcID, workerGroupName)
		if err != nil {
			return err
		}
	}

	if id, exists := ac.cloudConfig[ControlPlaneSecurityGroupIDKey]; exists {
		if controlPlaneGroupIDStr, ok := id.(string); ok && controlPlaneGroupIDStr != "" {
			controlPlaneGroupID = &controlPlaneGroupIDStr
		} else {
			return errors.New("Control Plane Security Group ID must be a valid non-empty string")
		}
	} else {
		controlPlaneGroupName := withInfraIDPrefix(ac.controlPlaneSGSuffix)

		controlPlaneGroupID, err = ac.getSecurityGroupName(ctx, vpcID, controlPlaneGroupName)
		if err != nil {
			return err
		}
	}

	err = ac.createClusterSGRule(ctx, workerGroupID, workerGroupID, port, protocol, internalTraffic+" between the workers")
	if err != nil {
		return err
	}

	err = ac.createClusterSGRule(ctx, workerGroupID, controlPlaneGroupID, port, protocol,
		internalTraffic+" from worker to control plane nodes")
	if err != nil {
		return err
	}

	return ac.createClusterSGRule(ctx, controlPlaneGroupID, workerGroupID, port, protocol,
		internalTraffic+" from control plane to worker nodes")
}

func (ac *awsCloud) createPublicSGRule(ctx context.Context, groupID *string, port uint16, protocol, description string) error {
	ipPermissions := []types.IpPermission{
		{
			FromPort:   ptr.To(int32(port)),
			ToPort:     ptr.To(int32(port)),
			IpProtocol: ptr.To(protocol),
			IpRanges: []types.IpRange{
				{
					CidrIp:      ptr.To("0.0.0.0/0"),
					Description: ptr.To(description),
				},
			},
		},
	}

	return ac.authorizeSecurityGroupIngress(ctx, groupID, ipPermissions)
}

func (ac *awsCloud) createGatewaySG(ctx context.Context, vpcID string, ports []api.PortSpec) (string, error) {
	groupName := ac.withAWSInfo(withInfraIDPrefix("-submariner-gw-sg"))

	gatewayGroupID, err := ac.getSecurityGroupName(ctx, vpcID, groupName)
	if err != nil {
		if !isNotFoundError(err) {
			return "", err
		}

		input := &ec2.CreateSecurityGroupInput{
			GroupName:   &groupName,
			Description: ptr.To("Submariner Gateway"),
			VpcId:       &vpcID,
			TagSpecifications: []types.TagSpecification{
				{
					ResourceType: types.ResourceTypeSecurityGroup,
					Tags: []types.Tag{
						ec2Tag("Name", groupName),
						ec2Tag(ac.withAWSInfo("kubernetes.io/cluster/{infraID}"), "owned"),
					},
				},
			},
		}

		result, err := ac.client.CreateSecurityGroup(ctx, input)

		if err != nil && !isAWSError(err, "InvalidGroup.Duplicate") {
			return "", errors.Wrap(err, "error creating AWS security group")
		}

		gatewayGroupID = result.GroupId
	}

	for _, port := range ports {
		err = ac.createPublicSGRule(ctx, gatewayGroupID, port.Port, port.Protocol, "Public Submariner traffic")
		if err != nil {
			return "", err
		}
	}

	return groupName, nil
}

func gatewayDeletionRetriable(err error) bool {
	return isAWSError(err, "DependencyViolation")
}

func (ac *awsCloud) deleteGatewaySG(ctx context.Context, vpcID string) error {
	groupName := ac.withAWSInfo(withInfraIDPrefix("-submariner-gw-sg"))

	gatewayGroupID, err := ac.getSecurityGroupName(ctx, vpcID, groupName)
	if err != nil {
		if isNotFoundError(err) {
			return nil
		}

		return err
	}

	backoff := wait.Backoff{
		Steps:    30,
		Duration: 500 * time.Millisecond,
		Factor:   1.2,
		Cap:      10 * time.Minute,
	}

	err = retry.OnError(backoff, gatewayDeletionRetriable, func() error {
		_, err = ac.client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
			GroupId: gatewayGroupID,
		})

		return err //nolint:wrapcheck // Let the caller wrap it.
	})

	if isAWSError(err, "InvalidPermission.NotFound") {
		return nil
	}

	return errors.Wrap(err, "error deleting AWS security group")
}

func (ac *awsCloud) revokePortsInCluster(ctx context.Context, vpcID string) error {
	var workerGroup, controlPlaneGroup types.SecurityGroup
	var err error

	if id, exists := ac.cloudConfig[WorkerSecurityGroupIDKey]; exists {
		if workerGroupIDStr, ok := id.(string); ok && workerGroupIDStr != "" {
			workerGroup, err = ac.getSecurityGroupByID(ctx, workerGroupIDStr)
			if err != nil {
				return errors.Wrap(err, "unable to get Worker Security Group by ID")
			}
		} else {
			return errors.New("Worker Security Group ID must be a valid non-empty string")
		}
	} else {
		workerGroupName := withInfraIDPrefix(ac.nodeSGSuffix)

		workerGroup, err = ac.getSecurityGroup(ctx, vpcID, workerGroupName)
		if err != nil {
			return err
		}
	}

	if id, exists := ac.cloudConfig[ControlPlaneSecurityGroupIDKey]; exists {
		if controlPlaneGroupIDStr, ok := id.(string); ok && controlPlaneGroupIDStr != "" {
			controlPlaneGroup, err = ac.getSecurityGroupByID(ctx, controlPlaneGroupIDStr)
			if err != nil {
				return errors.Wrap(err, "unable to get Control Plane Security Group by ID")
			}
		} else {
			return errors.New("Control Plane Security Group ID must be a valid non-empty string")
		}
	} else {
		controlPlaneGroupName := withInfraIDPrefix(ac.controlPlaneSGSuffix)

		controlPlaneGroup, err = ac.getSecurityGroup(ctx, vpcID, controlPlaneGroupName)
		if err != nil {
			return err
		}
	}

	err = ac.revokePortsFromGroup(ctx, &workerGroup)
	if err != nil {
		return err
	}

	return ac.revokePortsFromGroup(ctx, &controlPlaneGroup)
}

func (ac *awsCloud) revokePortsFromGroup(ctx context.Context, group *types.SecurityGroup) error {
	var permissionsToRevoke []types.IpPermission

	for perm := range group.IpPermissions {
		for i := range group.IpPermissions[perm].UserIdGroupPairs {
			groupPair := group.IpPermissions[perm].UserIdGroupPairs[i]
			if groupPair.Description != nil && strings.Contains(*groupPair.Description, internalTraffic) {
				permissionsToRevoke = append(permissionsToRevoke, group.IpPermissions[perm])
				break
			}
		}
	}

	if len(permissionsToRevoke) == 0 {
		return nil
	}

	input := &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       group.GroupId,
		IpPermissions: permissionsToRevoke,
	}

	_, err := ac.client.RevokeSecurityGroupIngress(ctx, input)

	return errors.Wrap(err, "error revoking AWS security group ingress")
}

func withInfraIDPrefix(s string) string {
	return "{infraID}" + s
}
