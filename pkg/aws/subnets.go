package aws

import "github.com/aws/aws-sdk-go/service/ec2"

var (
	tagSubmarinerGatgeway = ec2Tag("submariner.io/gateway", "")
	tagInternalELB        = ec2Tag("kubernetes.io/role/internal-elb", "")
)

func (ac *awsCloud) findPublicSubnet(vpcID string, filter *ec2.Filter) (*ec2.Subnet, error) {
	filters := []*ec2.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByCurrentCluster(),
		filter,
	}

	result, err := ac.client.DescribeSubnets(&ec2.DescribeSubnetsInput{Filters: filters})
	if err != nil {
		return nil, err
	}

	if len(result.Subnets) == 0 {
		return nil, newNotFoundError("public subnet")
	}

	return result.Subnets[0], nil
}

func (ac *awsCloud) getPublicSubnet(vpcID string) (*ec2.Subnet, error) {
	return ac.findPublicSubnet(vpcID, ac.filterByName("{infraID}-public-{region}*"))
}

func (ac *awsCloud) getTaggedPublicSubnet(vpcID string) (*ec2.Subnet, error) {
	subnet, err := ac.findPublicSubnet(vpcID, ec2FilterByTag(tagSubmarinerGatgeway))

	if err != nil {
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, err
	}
	return subnet, nil
}

func (ac *awsCloud) tagPublicSubnet(subnetID *string) error {
	_, err := ac.client.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{subnetID},
		Tags: []*ec2.Tag{
			tagInternalELB,
			tagSubmarinerGatgeway,
		},
	})

	return err
}

func (ac *awsCloud) untagPublicSubnet(subnetID *string) error {
	_, err := ac.client.DeleteTags(&ec2.DeleteTagsInput{
		Resources: []*string{subnetID},
		Tags: []*ec2.Tag{
			tagInternalELB,
			tagSubmarinerGatgeway,
		},
	})

	return err
}
