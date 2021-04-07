package aws

import "github.com/aws/aws-sdk-go/service/ec2"

var (
	tagSubmarinerGatgeway = ec2Tag("submariner.io/gateway", "")
	tagInternalELB        = ec2Tag("kubernetes.io/role/internal-elb", "")
)

func filterTaggedSubnets(subnets []*ec2.Subnet) ([]*ec2.Subnet, []*ec2.Subnet) {
	var taggedSubnets []*ec2.Subnet
	var untaggedSubnets []*ec2.Subnet
	for _, subnet := range subnets {
		if hasTag(subnet.Tags, tagSubmarinerGatgeway) {
			taggedSubnets = append(taggedSubnets, subnet)
		} else {
			untaggedSubnets = append(untaggedSubnets, subnet)
		}
	}

	return taggedSubnets, untaggedSubnets
}

func (ac *awsCloud) findPublicSubnets(vpcID string, filter *ec2.Filter) ([]*ec2.Subnet, error) {
	filters := []*ec2.Filter{
		ec2Filter("vpc-id", vpcID),
		ac.filterByCurrentCluster(),
		filter,
	}

	result, err := ac.client.DescribeSubnets(&ec2.DescribeSubnetsInput{Filters: filters})
	if err != nil {
		return nil, err
	}

	return result.Subnets, nil
}

func (ac *awsCloud) getPublicSubnets(vpcID string) ([]*ec2.Subnet, error) {
	return ac.findPublicSubnets(vpcID, ac.filterByName("{infraID}-public-{region}*"))
}

func (ac *awsCloud) getTaggedPublicSubnets(vpcID string) ([]*ec2.Subnet, error) {
	return ac.findPublicSubnets(vpcID, ec2FilterByTag(tagSubmarinerGatgeway))
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
