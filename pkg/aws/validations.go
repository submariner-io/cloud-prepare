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
	"net"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
)

const permissionsTest = "permissions-test"

func determinePermissionError(err error, operation string) error {
	if err == nil || isAWSError(err, "DryRunOperation") {
		return nil
	} else if isAWSError(err, "UnauthorizedOperation") {
		return fmt.Errorf("no permission to %s", operation)
	}

	return errors.Wrapf(err, "error while checking permissions for %s", operation)
}

func (ac *awsCloud) validateCreateSecGroup(vpcID string) error {
	input := &ec2.CreateSecurityGroupInput{
		DryRun:      aws.Bool(true),
		GroupName:   aws.String(permissionsTest),
		Description: aws.String(permissionsTest),
		VpcId:       aws.String(vpcID),
	}

	_, err := ac.client.CreateSecurityGroup(context.TODO(), input)

	return determinePermissionError(err, "create security group")
}

func (ac *awsCloud) validateCreateSecGroupRule(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	input := &ec2.AuthorizeSecurityGroupIngressInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.AuthorizeSecurityGroupIngress(context.TODO(), input)

	return determinePermissionError(err, "authorize security group ingress")
}

func (ac *awsCloud) validateCreateTag(subnetID string) error {
	_, err := ac.client.CreateTags(context.TODO(), &ec2.CreateTagsInput{
		DryRun:    aws.Bool(true),
		Resources: []string{subnetID},
		Tags: []types.Tag{
			tagSubmarinerGateway,
		},
	})

	return determinePermissionError(err, "create tags on subnets")
}

func (ac *awsCloud) validateDescribeInstanceTypeOfferings() error {
	_, err := ac.client.DescribeInstanceTypeOfferings(context.TODO(), &ec2.DescribeInstanceTypeOfferingsInput{
		DryRun: aws.Bool(true),
	})

	return determinePermissionError(err, "describe instance type offerings")
}

func (ac *awsCloud) validateDeleteSecGroup(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	input := &ec2.DeleteSecurityGroupInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.DeleteSecurityGroup(context.TODO(), input)

	return determinePermissionError(err, "delete security group")
}

func (ac *awsCloud) validateDeleteSecGroupRule(vpcID string) error {
	workerGroupID, err := ac.getSecurityGroupID(vpcID, "{infraID}-worker-sg")
	if err != nil {
		return err
	}

	input := &ec2.RevokeSecurityGroupIngressInput{
		DryRun:  aws.Bool(true),
		GroupId: workerGroupID,
	}

	_, err = ac.client.RevokeSecurityGroupIngress(context.TODO(), input)

	return determinePermissionError(err, "revoke security group ingress")
}

func (ac *awsCloud) validateRemoveTag(subnetID *string) error {
	_, err := ac.client.DeleteTags(context.TODO(), &ec2.DeleteTagsInput{
		DryRun:    aws.Bool(true),
		Resources: []string{*subnetID},
		Tags: []types.Tag{
			tagSubmarinerGateway,
		},
	})

	return determinePermissionError(err, "delete tags from subnets")
}

// validatePeeringPrerequisites checks info to determine if a VPC-Peering should work
func (ac *awsCloud) validatePeeringPrerequisites(target *awsCloud, reporter api.Reporter) error {
	reporter.Started("Validating VPC Peering pre-requisites")

	srcVpc, err := ac.getVpc()
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to validate vpc peering prerequisites for source")
	}

	targetVpc, err := target.getVpc()
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to validate vpc peering prerequisites for target")
	}

	overlap, err := checkVpcOverlap(srcVpc, targetVpc)
	if err != nil {
		reporter.Failed(err)
		return err
	} else if overlap {
		err = errors.Errorf("source [%v] and target [%v] CIDR Blocks must be different", srcVpc.CidrBlock, targetVpc.CidrBlock)
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Validated VPC Peering pre-requisites")
	return nil
}

// checkVpcOverlap checks CIDR Blocks of networks A and B overlaps
func checkVpcOverlap(a *types.Vpc, b *types.Vpc) (bool, error) {
	_, netA, err := net.ParseCIDR(*a.CidrBlock)
	if err != nil {
		return false, err
	}

	_, netB, err := net.ParseCIDR(*b.CidrBlock)
	if err != nil {
		return false, err
	}

	return netA.Contains(netB.IP) || netB.Contains(netA.IP), nil
}
