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
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/submariner-io/cloud-prepare/pkg/api"
	"strings"
)

func (ac *awsCloud) createAWSPeering(target *awsCloud, reporter api.Reporter) error {
	reporter.Started("Creating VPC Peering between %v/%v and %v/%v", ac.infraID, ac.region, target.infraID, target.region)
	err := ac.validatePeeringPrerequisites(target, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to validate vpc peering prerequisites")
	}
	sourceVpcId, err := ac.getVpcID()
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to retrieve source VPC ID")
	}
	targetVpcId, err := target.getVpcID()
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to retrieve target VPC ID")
	}
	peering, err := ac.requestPeering(sourceVpcId, targetVpcId, target, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to request VPC peering")
	}
	err = target.acceptPeering(peering.VpcPeeringConnectionId, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to accept VPC peering")
	}
	err = ac.createRoutesForPeering(target, sourceVpcId, targetVpcId, peering, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to create routes for VPC peering")
	}
	reporter.Succeeded("Created VPC Peering")
	return nil
}

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
	if strings.Compare(*srcVpc.CidrBlock, *targetVpc.CidrBlock) == 0 {
		err = errors.Errorf("source [%v] and target [%v] CIDR Blocks must be different", srcVpc.CidrBlock, targetVpc.CidrBlock)
		reporter.Failed(err)
		return err
	}
	reporter.Succeeded("Validated VPC Peering pre-requisites")
	return nil
}

func (ac *awsCloud) requestPeering(srcVpcId, targetVpcId string, target *awsCloud, reporter api.Reporter) (*types.VpcPeeringConnection, error) {
	reporter.Started("Requesting VPC Peering")
	input := &ec2.CreateVpcPeeringConnectionInput{
		VpcId:      &srcVpcId,
		PeerVpcId:  &targetVpcId,
		PeerRegion: &target.region,
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeVpcPeeringConnection,
				Tags: []types.Tag{
					ec2Tag("Name", fmt.Sprintf("%v-%v", ac.infraID, target.infraID)),
				},
			},
		},
	}
	output, err := ac.client.CreateVpcPeeringConnection(context.TODO(), input)
	if err != nil {
		reporter.Failed(err)
		return nil, errors.Wrapf(err, "unable to request VPC peering")
	}
	peering := output.VpcPeeringConnection
	reporter.Succeeded("Requested VPC Peering with ID %v", peering.VpcPeeringConnectionId)
	return peering, nil
}

func (ac *awsCloud) acceptPeering(peeringId *string, reporter api.Reporter) error {
	reporter.Started("Accepting VPC Peering")
	input := &ec2.AcceptVpcPeeringConnectionInput{
		VpcPeeringConnectionId: peeringId,
	}
	_, err := ac.client.AcceptVpcPeeringConnection(context.TODO(), input)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to accept VPC peering connection %v", peeringId)
	}
	reporter.Succeeded("Accepted VPC Peering with id: %v", peeringId)
	return nil
}

func (ac *awsCloud) createRoutesForPeering(target *awsCloud, srcVpcId, targetVpcId string, peering *types.VpcPeeringConnection, reporter api.Reporter) error {
	reporter.Started("Create VPC Peering")

	routeTableId, err := ac.getRouteTableId(srcVpcId, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to create route for %v", srcVpcId)
	}
	input := &ec2.CreateRouteInput{
		RouteTableId:           routeTableId,
		DestinationCidrBlock:   peering.AccepterVpcInfo.CidrBlock,
		VpcPeeringConnectionId: peering.VpcPeeringConnectionId,
	}
	_, err = ac.client.CreateRoute(context.TODO(), input)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to create route for %v", srcVpcId)
	}
	routeTableId, err = target.getRouteTableId(targetVpcId, reporter)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to create route for %v", targetVpcId)
	}
	input = &ec2.CreateRouteInput{
		RouteTableId:           routeTableId,
		DestinationCidrBlock:   peering.RequesterVpcInfo.CidrBlock,
		VpcPeeringConnectionId: peering.VpcPeeringConnectionId,
	}
	_, err = target.client.CreateRoute(context.TODO(), input)
	if err != nil {
		reporter.Failed(err)
		return errors.Wrapf(err, "unable to create route for %v", targetVpcId)
	}
	reporter.Succeeded("Created Routes for VPC Peering connection %v", peering.VpcPeeringConnectionId)
	return nil
}

func (ac *awsCloud) getRouteTableId(vpcId string, reporter api.Reporter) (*string, error) {
	reporter.Started("Getting RouteTableID")
	vpcIdKeyName := "vpc-id"
	associationKey := "association.main"
	input := &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name: &vpcIdKeyName,
				Values: []string{
					vpcId,
				},
			},
			{
				Name: &associationKey,
				Values: []string{
					"true",
				},
			},
		},
	}
	output, err := ac.client.DescribeRouteTables(context.TODO(), input)
	if err != nil {
		reporter.Failed(err)
		return nil, err
	}
	routeTableId := output.RouteTables[0].RouteTableId
	reporter.Succeeded("Retrieved RouteTableID %v", routeTableId)
	return routeTableId, nil
}
