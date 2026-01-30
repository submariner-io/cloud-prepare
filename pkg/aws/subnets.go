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

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
)

var (
	tagSubmarinerGateway = ec2Tag("submariner.io/gateway", "")
	tagInternalELB       = ec2Tag("kubernetes.io/role/internal-elb", "")
)

func filterSubnets(subnets []types.Subnet, filterFunc func(subnet *types.Subnet) (bool, error)) ([]types.Subnet, error) {
	var filteredSubnets []types.Subnet

	for i := range subnets {
		subnet := &subnets[i]

		filterResult, err := filterFunc(subnet)
		if err != nil {
			return nil, err
		}

		if filterResult {
			filteredSubnets = append(filteredSubnets, *subnet)
		}
	}

	return filteredSubnets, nil
}

func subnetTagged(subnet *types.Subnet) bool {
	return hasTag(subnet.Tags, tagSubmarinerGateway)
}

func (ac *awsCloud) findSubnetsByFilter(ctx context.Context, vpcID string, filter types.Filter) ([]types.Subnet, error) {
	ownedFilters := ac.filterByCurrentCluster()
	var err error
	var result *ec2.DescribeSubnetsOutput

	for i := range ownedFilters {
		filters := []types.Filter{
			ec2Filter("vpc-id", vpcID),
			ownedFilters[i],
			filter,
		}

		result, err = ac.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{Filters: filters})
		if err != nil {
			return nil, errors.Wrap(err, "error describing AWS subnets")
		}

		if len(result.Subnets) != 0 {
			break
		}
	}

	return result.Subnets, nil
}

func (ac *awsCloud) findPublicSubnets(ctx context.Context, vpcID string, filter types.Filter) ([]types.Subnet, error) {
	if len(ac.publicSubnetList) == 0 {
		publicSubnets, err := ac.findSubnetsByFilter(ctx, vpcID, filter)
		return publicSubnets, errors.Wrap(err, "unable to find public subnets")
	}

	var publicSubnets []types.Subnet

	for _, id := range ac.publicSubnetList {
		subnet, err := ac.getSubnetByID(ctx, id)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to find subnet with ID %q", id)
		}

		publicSubnets = append(publicSubnets, *subnet)
	}

	return publicSubnets, nil
}

func (ac *awsCloud) getSubnetsSupportingInstanceType(ctx context.Context, subnets []types.Subnet,
	instanceType string,
) ([]types.Subnet, error) {
	return filterSubnets(subnets, func(subnet *types.Subnet) (bool, error) {
		output, err := ac.client.DescribeInstanceTypeOfferings(ctx, &ec2.DescribeInstanceTypeOfferingsInput{
			LocationType: types.LocationTypeAvailabilityZone,
			Filters: []types.Filter{
				ec2Filter("location", *subnet.AvailabilityZone),
				ec2Filter("instance-type", instanceType),
			},
		})
		if err != nil {
			return false, err //nolint:wrapcheck // Let the caller wrap it.
		}

		return len(output.InstanceTypeOfferings) > 0, nil
	})
}

func (ac *awsCloud) tagPublicSubnet(ctx context.Context, subnetID *string) error {
	_, err := ac.client.CreateTags(ctx, &ec2.CreateTagsInput{
		Resources: []string{*subnetID},
		Tags: []types.Tag{
			tagInternalELB,
			tagSubmarinerGateway,
		},
	})

	return errors.Wrap(err, "error creating AWS tag")
}

func (ac *awsCloud) untagPublicSubnet(ctx context.Context, subnetID *string) error {
	_, err := ac.client.DeleteTags(ctx, &ec2.DeleteTagsInput{
		Resources: []string{*subnetID},
		Tags: []types.Tag{
			tagInternalELB,
			tagSubmarinerGateway,
		},
	})

	return errors.Wrap(err, "error deleting AWS tag")
}

func (ac *awsCloud) getSubnetByID(ctx context.Context, subnetID string) (*types.Subnet, error) {
	output, err := ac.client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
		SubnetIds: []string{subnetID},
	})
	if err != nil {
		return nil, errors.Wrapf(err, "unable to describe subnet %s", subnetID)
	}

	if len(output.Subnets) == 0 {
		return nil, errors.New("subnet not found")
	}

	return &output.Subnets[0], nil
}

func (ac *awsCloud) publicFilter() types.Filter {
	return ac.filterByName("{infraID}*-public-{region}*")
}

func submarinerGatewayFilter() types.Filter {
	return ec2FilterByTag(tagSubmarinerGateway)
}
