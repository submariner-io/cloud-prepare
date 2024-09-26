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

func (ac *awsCloud) getSecurityGroupName(vpcID, name string) (*string, error) {
	group, err := ac.getSecurityGroup(vpcID, name)
	if err != nil {
		return nil, err
	}

	return group.GroupId, nil
}

func (ac *awsCloud) getSecurityGroupByID(groupID string) (types.SecurityGroup, error) {
	output, err := ac.client.DescribeSecurityGroups(context.TODO(), &ec2.DescribeSecurityGroupsInput{
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

func (ac *awsCloud) getSecurityGroup(vpcID, name string) (types.SecurityGroup, error) {
	filters := []types.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByName(name),
	}

	result, err := ac.client.DescribeSecurityGroups(context.TODO(), &ec2.DescribeSecurityGroupsInput{
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

func (ac *awsCloud) authorizeSecurityGroupIngress(groupID *string, ipPermissions []types.IpPermission) error {
	input := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       groupID,
		IpPermissions: ipPermissions,
	}

	_, err := ac.client.AuthorizeSecurityGroupIngress(context.TODO(), input)
	if isAWSError(err, "InvalidPermission.Duplicate") {
		return nil
	}

	return errors.Wrap(err, "error authorizing AWS security groups ingress")
}

func (ac *awsCloud) createClusterSGRule(srcGroup, destGroup *string, port uint16, protocol, description string) error {
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

	return ac.authorizeSecurityGroupIngress(destGroup, ipPermissions)
}

func (ac *awsCloud) allowPortInCluster(vpcID string, port uint16, protocol string) error {
	var workerGroupID, controlPlaneGroupID *string
	var err error

	if id, exists := ac.cloudConfig[WorkerSecurityGroupIDKey]; exists {
		if workerGroupIDStr, ok := id.(string); ok && workerGroupIDStr != "" {
			workerGroupID = &workerGroupIDStr
		} else {
			return errors.New("Worker Security Group ID must be a valid non-empty string")
		}
	} else {
		workerGroupID, err = ac.getSecurityGroupName(vpcID, "{infraID}"+ac.nodeSGSuffix)
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
		controlPlaneGroupID, err = ac.getSecurityGroupName(vpcID, "{infraID}"+ac.controlPlaneSGSuffix)
		if err != nil {
			return err
		}
	}

	err = ac.createClusterSGRule(workerGroupID, workerGroupID, port, protocol, fmt.Sprintf("%s between the workers", internalTraffic))
	if err != nil {
		return err
	}

	err = ac.createClusterSGRule(workerGroupID, controlPlaneGroupID, port, protocol,
		fmt.Sprintf("%s from worker to master nodes", internalTraffic))
	if err != nil {
		return err
	}

	return ac.createClusterSGRule(controlPlaneGroupID, workerGroupID, port, protocol,
		fmt.Sprintf("%s from master to worker nodes", internalTraffic))
}

func (ac *awsCloud) createPublicSGRule(groupID *string, port uint16, protocol, description string) error {
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

	return ac.authorizeSecurityGroupIngress(groupID, ipPermissions)
}

func (ac *awsCloud) createGatewaySG(vpcID string, ports []api.PortSpec) (string, error) {
	groupName := ac.withAWSInfo("{infraID}-submariner-gw-sg")

	gatewayGroupID, err := ac.getSecurityGroupName(vpcID, groupName)
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
					},
				},
			},
		}

		result, err := ac.client.CreateSecurityGroup(context.TODO(), input)

		if err != nil && !isAWSError(err, "InvalidGroup.Duplicate") {
			return "", errors.Wrap(err, "error creating AWS security group")
		}

		gatewayGroupID = result.GroupId
	}

	for _, port := range ports {
		err = ac.createPublicSGRule(gatewayGroupID, port.Port, port.Protocol, "Public Submariner traffic")
		if err != nil {
			return "", err
		}
	}

	return groupName, nil
}

func gatewayDeletionRetriable(err error) bool {
	return isAWSError(err, "DependencyViolation")
}

func (ac *awsCloud) deleteGatewaySG(vpcID string) error {
	groupName := ac.withAWSInfo("{infraID}-submariner-gw-sg")

	gatewayGroupID, err := ac.getSecurityGroupName(vpcID, groupName)
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
		_, err = ac.client.DeleteSecurityGroup(context.TODO(), &ec2.DeleteSecurityGroupInput{
			GroupId: gatewayGroupID,
		})

		return err //nolint:wrapcheck // Let the caller wrap it.
	})

	if isAWSError(err, "InvalidPermission.NotFound") {
		return nil
	}

	return errors.Wrap(err, "error deleting AWS security group")
}

func (ac *awsCloud) revokePortsInCluster(vpcID string) error {
	var workerGroup, controlPlaneGroup types.SecurityGroup
	var err error

	if id, exists := ac.cloudConfig[WorkerSecurityGroupIDKey]; exists {
		if workerGroupIDStr, ok := id.(string); ok && workerGroupIDStr != "" {
			workerGroup, err = ac.getSecurityGroupByID(workerGroupIDStr)
			if err != nil {
				return errors.Wrap(err, "unable to get Worker Security Group by ID")
			}
		} else {
			return errors.New("Worker Security Group ID must be a valid non-empty string")
		}
	} else {
		workerGroup, err = ac.getSecurityGroup(vpcID, "{infraID}"+ac.nodeSGSuffix)
		if err != nil {
			return err
		}
	}

	if id, exists := ac.cloudConfig[ControlPlaneSecurityGroupIDKey]; exists {
		if controlPlaneGroupIDStr, ok := id.(string); ok && controlPlaneGroupIDStr != "" {
			controlPlaneGroup, err = ac.getSecurityGroupByID(controlPlaneGroupIDStr)
			if err != nil {
				return errors.Wrap(err, "unable to get Control Plane Security Group by ID")
			}
		} else {
			return errors.New("Control Plane Security Group ID must be a valid non-empty string")
		}
	} else {
		controlPlaneGroup, err = ac.getSecurityGroup(vpcID, "{infraID}"+ac.controlPlaneSGSuffix)
		if err != nil {
			return err
		}
	}

	err = ac.revokePortsFromGroup(&workerGroup)
	if err != nil {
		return err
	}

	return ac.revokePortsFromGroup(&controlPlaneGroup)
}

func (ac *awsCloud) revokePortsFromGroup(group *types.SecurityGroup) error {
	var permissionsToRevoke []types.IpPermission

	for _, permission := range group.IpPermissions {
		for _, groupPair := range permission.UserIdGroupPairs {
			if groupPair.Description != nil && strings.Contains(*groupPair.Description, internalTraffic) {
				permissionsToRevoke = append(permissionsToRevoke, permission)
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

	_, err := ac.client.RevokeSecurityGroupIngress(context.TODO(), input)

	return errors.Wrap(err, "error revoking AWS security group ingress")
}
