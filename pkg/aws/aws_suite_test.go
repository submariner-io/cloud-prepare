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

package aws_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"github.com/submariner-io/cloud-prepare/pkg/aws/client/fake"
	"k8s.io/utils/ptr"
)

const (
	infraID              = "test-infra"
	region               = "test-region"
	vpcID                = "test-vpc"
	workerGroupID        = "worker-group"
	masterGroupID        = "master-group"
	gatewayGroupID       = "gateway-group"
	internalTraffic      = "Internal Submariner traffic"
	availabilityZone1    = "availability-zone-1"
	availabilityZone2    = "availability-zone-2"
	subnetID1            = "subnet-1"
	subnetID2            = "subnet-2"
	instanceImageID      = "test-image"
	masterSGName         = infraID + "-master-sg"
	workerSGName         = infraID + "-worker-sg"
	gatewaySGName        = infraID + "-submariner-gw-sg"
	clusterFilterTagName = "tag:kubernetes.io/cluster/" + infraID
)

var internalTrafficDesc = fmt.Sprintf("Should contain %q", internalTraffic)

func TestAWS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AWS Suite")
}

type fakeAWSClientBase struct {
	awsClient                        *fake.MockInterface
	vpcID                            string
	describeSubnetsErr               error
	authorizeSecurityGroupIngressErr error
	createTagsErr                    error
	describeInstanceTypeOfferingsErr error
}

func (f *fakeAWSClientBase) beforeEach() {
	f.awsClient = fake.NewMockInterface(GinkgoT())
	f.vpcID = vpcID
	f.describeSubnetsErr = nil
	f.authorizeSecurityGroupIngressErr = nil
	f.createTagsErr = nil
	f.describeInstanceTypeOfferingsErr = nil
}

func (f *fakeAWSClientBase) afterEach() {
	f.awsClient.AssertExpectations(GinkgoT())
}

func (f *fakeAWSClientBase) expectDescribeSecurityGroups(name, groupID string, ipPermissions ...types.IpPermission) {
	f.awsClient.EXPECT().DescribeSecurityGroups(mock.Anything, newDescribeSecurityGroupsInput(f.vpcID, name)).
		Return(newDescribeSecurityGroupsOutput(groupID, ipPermissions...), nil).Maybe()
}

func (f *fakeAWSClientBase) expectDescribeSecurityGroupsFailure(name string, err error) {
	f.awsClient.EXPECT().DescribeSecurityGroups(mock.Anything, newDescribeSecurityGroupsInput(f.vpcID, name)).
		Return(nil, err).Maybe()
}

func (f *fakeAWSClientBase) expectDescribeVpcs(vpcID string) {
	var vpcs []types.Vpc
	if vpcID != "" {
		vpcs = []types.Vpc{
			{
				VpcId: ptr.To(vpcID),
			},
		}
	}

	f.awsClient.EXPECT().DescribeVpcs(mock.Anything, mock.MatchedBy(((&filtersMatcher{expectedFilters: []types.Filter{{
		Name:   ptr.To("tag:Name"),
		Values: []string{infraID + "-vpc"},
	}, {
		Name:   ptr.To(clusterFilterTagName),
		Values: []string{"owned"},
	}}}).Matches))).Return(&ec2.DescribeVpcsOutput{Vpcs: vpcs}, nil).Maybe()
}

func (f *fakeAWSClientBase) expectValidateAuthorizeSecurityGroupIngress(authErr error) *mock.Call {
	return f.awsClient.EXPECT().AuthorizeSecurityGroupIngress(mock.Anything,
		mock.MatchedBy((&authorizeSecurityGroupIngressInputMatcher{ec2.AuthorizeSecurityGroupIngressInput{
			DryRun:  ptr.To(true),
			GroupId: ptr.To(workerGroupID),
		}}).Matches)).Return(&ec2.AuthorizeSecurityGroupIngressOutput{}, authErr).Call
}

func (f *fakeAWSClientBase) expectAuthorizeSecurityGroupIngress(srcGroup string, ipPerm *types.IpPermission) {
	f.awsClient.EXPECT().AuthorizeSecurityGroupIngress(mock.Anything,
		mock.MatchedBy((&authorizeSecurityGroupIngressInputMatcher{ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       ptr.To(srcGroup),
			IpPermissions: []types.IpPermission{*ipPerm},
		}}).Matches)).Return(&ec2.AuthorizeSecurityGroupIngressOutput{},
		f.authorizeSecurityGroupIngressErr)
}

func (f *fakeAWSClientBase) expectRevokeSecurityGroupIngress(groupID string, ipPermissions ...types.IpPermission) {
	f.awsClient.EXPECT().RevokeSecurityGroupIngress(mock.Anything, &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       ptr.To(groupID),
		IpPermissions: ipPermissions,
	}).Return(&ec2.RevokeSecurityGroupIngressOutput{}, nil)
}

func (f *fakeAWSClientBase) expectValidateRevokeSecurityGroupIngress(retErr error) {
	f.awsClient.EXPECT().RevokeSecurityGroupIngress(mock.Anything, &ec2.RevokeSecurityGroupIngressInput{
		DryRun:  ptr.To(true),
		GroupId: ptr.To(workerGroupID),
	}).Return(&ec2.RevokeSecurityGroupIngressOutput{}, retErr)
}

func (f *fakeAWSClientBase) expectDescribePublicSubnets(retSubnets ...types.Subnet) {
	f.awsClient.EXPECT().DescribeSubnets(mock.Anything, mock.MatchedBy(((&filtersMatcher{expectedFilters: []types.Filter{{
		Name:   ptr.To("tag:Name"),
		Values: []string{infraID + "-public-" + region + "*"},
	}, {
		Name:   ptr.To("vpc-id"),
		Values: []string{f.vpcID},
	}, {
		Name:   ptr.To(clusterFilterTagName),
		Values: []string{"owned"},
	}}}).Matches))).Return(&ec2.DescribeSubnetsOutput{Subnets: retSubnets}, f.describeSubnetsErr).Maybe()
}

func (f *fakeAWSClientBase) expectDescribeGatewaySubnets(retSubnets ...types.Subnet) {
	f.awsClient.EXPECT().DescribeSubnets(mock.Anything, mock.MatchedBy(((&filtersMatcher{expectedFilters: []types.Filter{{
		Name:   ptr.To("tag:submariner.io/gateway"),
		Values: []string{""},
	}, {
		Name:   ptr.To("vpc-id"),
		Values: []string{f.vpcID},
	}, {
		Name:   ptr.To(clusterFilterTagName),
		Values: []string{"owned"},
	}}}).Matches))).Return(&ec2.DescribeSubnetsOutput{Subnets: retSubnets}, f.describeSubnetsErr).Maybe()
}

func (f *fakeAWSClientBase) expectValidateCreateSecurityGroup() *mock.Call {
	return f.awsClient.EXPECT().CreateSecurityGroup(mock.Anything, mock.MatchedBy(func(in *ec2.CreateSecurityGroupInput) bool {
		return in.DryRun != nil && *in.DryRun
	})).Return(&ec2.CreateSecurityGroupOutput{}, nil).Call
}

func (f *fakeAWSClientBase) expectCreateSecurityGroup(name, retGroupID string) {
	f.awsClient.EXPECT().CreateSecurityGroup(mock.Anything, &ec2.CreateSecurityGroupInput{
		Description: ptr.To("Submariner Gateway"),
		GroupName:   ptr.To(name),
		VpcId:       ptr.To(f.vpcID),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeSecurityGroup,
				Tags: []types.Tag{
					{
						Key:   ptr.To("Name"),
						Value: ptr.To(name),
					},
				},
			},
		},
	}).Return(&ec2.CreateSecurityGroupOutput{GroupId: ptr.To(retGroupID)}, nil)
}

func (f *fakeAWSClientBase) expectDeleteSecurityGroup(groupID string) {
	f.awsClient.EXPECT().DeleteSecurityGroup(mock.Anything, &ec2.DeleteSecurityGroupInput{
		GroupId: ptr.To(groupID),
	}).Return(&ec2.DeleteSecurityGroupOutput{}, nil)
}

func (f *fakeAWSClientBase) expectValidateDeleteSecurityGroup() *mock.Call {
	return f.awsClient.EXPECT().DeleteSecurityGroup(mock.Anything, &ec2.DeleteSecurityGroupInput{
		DryRun:  ptr.To(true),
		GroupId: ptr.To(workerGroupID),
	}).Return(&ec2.DeleteSecurityGroupOutput{}, nil).Call
}

func (f *fakeAWSClientBase) expectValidateDescribeInstanceTypeOfferings() *mock.Call {
	return f.awsClient.EXPECT().DescribeInstanceTypeOfferings(mock.Anything,
		mock.MatchedBy(func(in *ec2.DescribeInstanceTypeOfferingsInput) bool {
			return in.DryRun != nil && *in.DryRun
		})).Return(&ec2.DescribeInstanceTypeOfferingsOutput{}, nil).Call
}

func (f *fakeAWSClientBase) expectDescribeInstanceTypeOfferings(instanceType, availabilityZone string,
	retOfferings ...types.InstanceTypeOffering,
) {
	f.awsClient.EXPECT().DescribeInstanceTypeOfferings(mock.Anything, mock.MatchedBy(((&filtersMatcher{expectedFilters: []types.Filter{{
		Name:   ptr.To("location"),
		Values: []string{availabilityZone},
	}, {
		Name:   ptr.To("instance-type"),
		Values: []string{instanceType},
	}}}).Matches))).Return(&ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: retOfferings},
		f.describeInstanceTypeOfferingsErr).Maybe()
}

func (f *fakeAWSClientBase) expectCreateTags(subnetID string, tagKeys ...string) {
	f.awsClient.EXPECT().CreateTags(mock.Anything, &ec2.CreateTagsInput{
		Resources: []string{subnetID},
		Tags:      makeTags(tagKeys),
	}).Return(&ec2.CreateTagsOutput{}, f.createTagsErr)
}

func (f *fakeAWSClientBase) expectCreateGatewayTags(subnetID string) {
	f.expectCreateTags(subnetID, "kubernetes.io/role/internal-elb", "submariner.io/gateway")
}

func (f *fakeAWSClientBase) expectValidateCreateTags() *mock.Call {
	return f.awsClient.EXPECT().CreateTags(mock.Anything, mock.MatchedBy(func(in *ec2.CreateTagsInput) bool {
		return in.DryRun != nil && *in.DryRun
	})).Return(&ec2.CreateTagsOutput{}, nil).Call
}

func (f *fakeAWSClientBase) expectDeleteTags(subnetID string, tagKeys ...string) {
	f.awsClient.EXPECT().DeleteTags(mock.Anything, &ec2.DeleteTagsInput{
		Resources: []string{subnetID},
		Tags:      makeTags(tagKeys),
	}).Return(&ec2.DeleteTagsOutput{}, f.createTagsErr)
}

func (f *fakeAWSClientBase) expectDeleteGatewayTags(subnetID string) {
	f.expectDeleteTags(subnetID, "kubernetes.io/role/internal-elb", "submariner.io/gateway")
}

func (f *fakeAWSClientBase) expectValidateDeleteTags() *mock.Call {
	return f.awsClient.EXPECT().DeleteTags(mock.Anything, mock.MatchedBy(func(in *ec2.DeleteTagsInput) bool {
		return in.DryRun != nil && *in.DryRun
	})).Return(&ec2.DeleteTagsOutput{}, nil).Call
}

func (f *fakeAWSClientBase) expectDescribeInstances(retImageID string) {
	var reservations []types.Reservation
	if retImageID != "" {
		reservations = []types.Reservation{
			{
				Instances: []types.Instance{
					{
						ImageId: ptr.To(retImageID),
					},
				},
			},
		}
	}

	f.awsClient.EXPECT().DescribeInstances(mock.Anything, mock.MatchedBy(((&filtersMatcher{expectedFilters: []types.Filter{{
		Name:   ptr.To("tag:Name"),
		Values: []string{infraID + "-worker*"},
	}, {
		Name:   ptr.To("vpc-id"),
		Values: []string{f.vpcID},
	}, {
		Name:   ptr.To(clusterFilterTagName),
		Values: []string{"owned"},
	}}}).Matches))).Return(&ec2.DescribeInstancesOutput{Reservations: reservations}, nil).Maybe()
}

func makeTags(tagKeys []string) []types.Tag {
	tags := make([]types.Tag, len(tagKeys))
	for i := range tagKeys {
		tags[i] = types.Tag{
			Key:   ptr.To(tagKeys[i]),
			Value: ptr.To(""),
		}
	}

	return tags
}

func newSubnet(availabilityZone, subnetID string) types.Subnet {
	return types.Subnet{
		SubnetId:         ptr.To(subnetID),
		AvailabilityZone: ptr.To(availabilityZone),
		Tags: []types.Tag{
			{
				Key:   ptr.To("Name"),
				Value: ptr.To(subnetName(subnetID)),
			},
		},
	}
}

func subnetName(subnetID string) string {
	return "Subnet:" + subnetID
}

func newDescribeSecurityGroupsInput(vpcID, name string) *ec2.DescribeSecurityGroupsInput {
	return &ec2.DescribeSecurityGroupsInput{
		Filters: []types.Filter{
			{
				Name:   ptr.To("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   ptr.To("tag:Name"),
				Values: []string{name},
			},
		},
	}
}

func newDescribeSecurityGroupsOutput(groupID string, ipPermissions ...types.IpPermission) *ec2.DescribeSecurityGroupsOutput {
	if groupID == "" {
		return &ec2.DescribeSecurityGroupsOutput{}
	}

	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []types.SecurityGroup{
		{
			GroupId:       ptr.To(groupID),
			IpPermissions: ipPermissions,
		},
	}}
}

func newIPPermission(desc string) types.IpPermission {
	return types.IpPermission{
		UserIdGroupPairs: []types.UserIdGroupPair{
			{
				Description: ptr.To(desc),
			},
		},
	}
}

func newClusterSGRule(groupID string, port int32, protocol string) *types.IpPermission {
	return &types.IpPermission{
		FromPort:   ptr.To(port),
		ToPort:     ptr.To(port),
		IpProtocol: ptr.To(protocol),
		UserIdGroupPairs: []types.UserIdGroupPair{
			{
				GroupId:     ptr.To(groupID),
				Description: ptr.To(internalTrafficDesc),
			},
		},
	}
}

func newPublicSGRule(port int32, protocol string) *types.IpPermission {
	return &types.IpPermission{
		FromPort:   ptr.To(port),
		ToPort:     ptr.To(port),
		IpProtocol: ptr.To(protocol),
		IpRanges: []types.IpRange{
			{
				CidrIp: ptr.To("0.0.0.0/0"),
			},
		},
	}
}

type authorizeSecurityGroupIngressInputMatcher struct {
	ec2.AuthorizeSecurityGroupIngressInput
}

func (m *authorizeSecurityGroupIngressInputMatcher) Matches(i interface{}) bool {
	o, ok := i.(*ec2.AuthorizeSecurityGroupIngressInput)
	Expect(ok).To(BeTrue())

	aInput := *o
	aIPPerms := aInput.IpPermissions
	aInput.IpPermissions = nil

	eInput := m.AuthorizeSecurityGroupIngressInput
	eIPPerms := eInput.IpPermissions
	eInput.IpPermissions = nil

	if !reflect.DeepEqual(&eInput, &aInput) && len(eIPPerms) != len(aIPPerms) {
		return false
	}

	if len(eIPPerms) == 0 {
		return true
	}

	copyIPPerm := func(in *types.IpPermission) *types.IpPermission {
		out := *in

		if in.UserIdGroupPairs != nil {
			out.UserIdGroupPairs = make([]types.UserIdGroupPair, len(in.UserIdGroupPairs))
			copy(out.UserIdGroupPairs, in.UserIdGroupPairs)

			for i := range out.UserIdGroupPairs {
				if out.UserIdGroupPairs[i].Description != nil && strings.Contains(*out.UserIdGroupPairs[i].Description, internalTraffic) {
					out.UserIdGroupPairs[i].Description = ptr.To(internalTrafficDesc)
				}
			}
		}

		if in.IpRanges != nil {
			out.IpRanges = make([]types.IpRange, len(in.IpRanges))
			copy(out.IpRanges, in.IpRanges)

			for i := range out.IpRanges {
				out.IpRanges[i].Description = nil
			}
		}

		return &out
	}

	return reflect.DeepEqual(&eIPPerms[0], copyIPPerm(&aIPPerms[0]))
}

type filtersMatcher struct {
	expectedFilters []types.Filter
}

func (m *filtersMatcher) Matches(i interface{}) bool {
	filtersValue := reflect.Indirect(reflect.ValueOf(i)).FieldByName("Filters")
	filters := filtersValue.Interface().([]types.Filter)

	expMap := map[string]types.Filter{}
	for i := range m.expectedFilters {
		expMap[*m.expectedFilters[i].Name] = m.expectedFilters[i]
	}

	for i := range filters {
		if filters[i].Name == nil {
			return false
		}

		expFilter, ok := expMap[*filters[i].Name]
		delete(expMap, *filters[i].Name)

		if !ok || !reflect.DeepEqual(filters[i].Values, expFilter.Values) {
			return false
		}
	}

	return len(expMap) == 0
}
