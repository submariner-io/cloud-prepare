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

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/mock"
	"github.com/submariner-io/cloud-prepare/pkg/aws/client/fake"
	"k8s.io/utils/ptr"
)

const (
	infraID                  = "test-infra"
	region                   = "test-region"
	vpcID                    = "test-vpc"
	workerGroupID            = "worker-group"
	masterGroupID            = "master-group"
	gatewayGroupID           = "gateway-group"
	internalTraffic          = "Internal Submariner traffic"
	availabilityZone1        = "availability-zone-1"
	availabilityZone2        = "availability-zone-2"
	subnetID1                = "subnet-1"
	subnetID2                = "subnet-2"
	instanceImageID          = "test-image"
	masterSGName             = infraID + "-master-sg"
	workerSGName             = infraID + "-worker-sg"
	gatewaySGName            = infraID + "-submariner-gw-sg"
	providerAWSTagPrefix     = "tag:sigs.k8s.io/cluster-api-provider-aws/cluster/"
	clusterFilterTagName     = "tag:kubernetes.io/cluster/" + infraID
	clusterFilterTagNameSigs = providerAWSTagPrefix + infraID
)

var internalTrafficDesc = fmt.Sprintf("Should contain %q", internalTraffic)

func TestAWS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AWS Suite")
}

type fakeAWSClientBase struct {
	awsClient                        *fake.MockInterface
	mockCtrl                         *gomock.Controller
	vpcID                            string
	subnets                          []types.Subnet
	describeSubnetsErr               error
	authorizeSecurityGroupIngressErr error
	createTagsErr                    error
	describeInstanceTypeOfferingsErr error
}

func (f *fakeAWSClientBase) beforeEach() {
	f.mockCtrl = gomock.NewController(GinkgoT())
	f.awsClient = fake.NewMockInterface(f.mockCtrl)
	f.vpcID = vpcID
	f.subnets = []types.Subnet{newSubnet(availabilityZone1, subnetID1), newSubnet(availabilityZone2, subnetID2)}
	f.describeSubnetsErr = nil
	f.authorizeSecurityGroupIngressErr = nil
	f.createTagsErr = nil
	f.describeInstanceTypeOfferingsErr = nil
}

func (f *fakeAWSClientBase) afterEach() {
	f.mockCtrl.Finish()
}

func (f *fakeAWSClientBase) expectDescribeSecurityGroups(name, groupID string, ipPermissions ...types.IpPermission) {
	f.awsClient.EXPECT().DescribeSecurityGroups(gomock.Any(), mock.Eq(newDescribeSecurityGroupsInput(f.vpcID, name))).
		Return(newDescribeSecurityGroupsOutput(groupID, ipPermissions...), nil).AnyTimes()
}

func (f *fakeAWSClientBase) expectDescribeSecurityGroupsFailure(name string, err error) {
	f.awsClient.EXPECT().DescribeSecurityGroups(gomock.Any(), mock.Eq(newDescribeSecurityGroupsInput(f.vpcID, name))).
		Return(nil, err).AnyTimes()
}

func (f *fakeAWSClientBase) expectDescribeVpcs(vpcID string) {
	var vpcs []types.Vpc
	if vpcID != "" {
		vpcs = []types.Vpc{
			{
				VpcId: awssdk.String(vpcID),
			},
		}
	}

	f.awsClient.EXPECT().DescribeVpcs(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("tag:Name"),
		Values: []string{infraID + "-vpc"},
	}, types.Filter{
		Name:   awssdk.String(clusterFilterTagName),
		Values: []string{"owned"},
	}, types.Filter{
		Name:   ptr.To(providerAWSTagPrefix + infraID),
		Values: []string{"owned"},
	})).Return(&ec2.DescribeVpcsOutput{Vpcs: vpcs}, nil).AnyTimes()
}

func (f *fakeAWSClientBase) expectValidateAuthorizeSecurityGroupIngress(authErr error) *gomock.Call {
	return f.awsClient.EXPECT().AuthorizeSecurityGroupIngress(gomock.Any(), mock.Eq(&ec2.AuthorizeSecurityGroupIngressInput{
		DryRun:  awssdk.Bool(true),
		GroupId: awssdk.String(workerGroupID),
	})).Return(&ec2.AuthorizeSecurityGroupIngressOutput{}, authErr)
}

func (f *fakeAWSClientBase) expectAuthorizeSecurityGroupIngress(srcGroup string, ipPerm *types.IpPermission) {
	f.awsClient.EXPECT().AuthorizeSecurityGroupIngress(gomock.Any(),
		eqAuthorizeSecurityGroupIngressInput(srcGroup, ipPerm)).Return(&ec2.AuthorizeSecurityGroupIngressOutput{},
		f.authorizeSecurityGroupIngressErr)
}

func (f *fakeAWSClientBase) expectRevokeSecurityGroupIngress(groupID string, ipPermissions ...types.IpPermission) {
	f.awsClient.EXPECT().RevokeSecurityGroupIngress(gomock.Any(), mock.Eq(&ec2.RevokeSecurityGroupIngressInput{
		GroupId:       awssdk.String(groupID),
		IpPermissions: ipPermissions,
	})).Return(&ec2.RevokeSecurityGroupIngressOutput{}, nil)
}

func (f *fakeAWSClientBase) expectValidateRevokeSecurityGroupIngress(retErr error) {
	f.awsClient.EXPECT().RevokeSecurityGroupIngress(gomock.Any(), mock.Eq(&ec2.RevokeSecurityGroupIngressInput{
		DryRun:  awssdk.Bool(true),
		GroupId: awssdk.String(workerGroupID),
	})).Return(&ec2.RevokeSecurityGroupIngressOutput{}, retErr)
}

func (f *fakeAWSClientBase) expectDescribePublicSubnets(retSubnets ...types.Subnet) {
	f.awsClient.EXPECT().DescribeSubnets(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("tag:Name"),
		Values: []string{infraID + "*-public-" + region + "*"},
	}, types.Filter{
		Name:   awssdk.String("vpc-id"),
		Values: []string{f.vpcID},
	}, types.Filter{
		Name:   awssdk.String(clusterFilterTagName),
		Values: []string{"owned"},
	})).Return(&ec2.DescribeSubnetsOutput{Subnets: retSubnets}, f.describeSubnetsErr).AnyTimes()
}

func (f *fakeAWSClientBase) expectDescribePublicSubnetsSigs(retSubnets ...types.Subnet) {
	f.awsClient.EXPECT().DescribeSubnets(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("tag:Name"),
		Values: []string{infraID + "*-public-" + region + "*"},
	}, types.Filter{
		Name:   awssdk.String("vpc-id"),
		Values: []string{f.vpcID},
	}, types.Filter{
		Name:   awssdk.String(clusterFilterTagNameSigs),
		Values: []string{"owned"},
	})).Return(&ec2.DescribeSubnetsOutput{Subnets: retSubnets}, f.describeSubnetsErr).AnyTimes()
}

func (f *fakeAWSClientBase) expectDescribeGatewaySubnets(retSubnets ...types.Subnet) {
	f.awsClient.EXPECT().DescribeSubnets(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("tag:submariner.io/gateway"),
		Values: []string{""},
	}, types.Filter{
		Name:   awssdk.String("vpc-id"),
		Values: []string{f.vpcID},
	}, types.Filter{
		Name:   awssdk.String(clusterFilterTagName),
		Values: []string{"owned"},
	})).Return(&ec2.DescribeSubnetsOutput{Subnets: retSubnets}, f.describeSubnetsErr).AnyTimes()
}

func (f *fakeAWSClientBase) expectValidateCreateSecurityGroup() *gomock.Call {
	return f.awsClient.EXPECT().CreateSecurityGroup(gomock.Any(), eqDryRun(&ec2.CreateSecurityGroupInput{})).
		Return(&ec2.CreateSecurityGroupOutput{}, nil)
}

func (f *fakeAWSClientBase) expectCreateSecurityGroup(name, retGroupID string) {
	f.awsClient.EXPECT().CreateSecurityGroup(gomock.Any(), mock.Eq(&ec2.CreateSecurityGroupInput{
		Description: awssdk.String("Submariner Gateway"),
		GroupName:   awssdk.String(name),
		VpcId:       awssdk.String(f.vpcID),
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeSecurityGroup,
				Tags: []types.Tag{
					{
						Key:   awssdk.String("Name"),
						Value: awssdk.String(name),
					},
				},
			},
		},
	})).Return(&ec2.CreateSecurityGroupOutput{GroupId: awssdk.String(retGroupID)}, nil)
}

func (f *fakeAWSClientBase) expectDeleteSecurityGroup(groupID string) {
	f.awsClient.EXPECT().DeleteSecurityGroup(gomock.Any(), mock.Eq(&ec2.DeleteSecurityGroupInput{
		GroupId: awssdk.String(groupID),
	})).Return(&ec2.DeleteSecurityGroupOutput{}, nil)
}

func (f *fakeAWSClientBase) expectValidateDeleteSecurityGroup() *gomock.Call {
	return f.awsClient.EXPECT().DeleteSecurityGroup(gomock.Any(), mock.Eq(&ec2.DeleteSecurityGroupInput{
		DryRun:  awssdk.Bool(true),
		GroupId: awssdk.String(workerGroupID),
	})).Return(&ec2.DeleteSecurityGroupOutput{}, nil)
}

func (f *fakeAWSClientBase) expectValidateDescribeInstanceTypeOfferings() *gomock.Call {
	return f.awsClient.EXPECT().DescribeInstanceTypeOfferings(gomock.Any(), eqDryRun(&ec2.DescribeInstanceTypeOfferingsInput{})).
		Return(&ec2.DescribeInstanceTypeOfferingsOutput{}, nil)
}

func (f *fakeAWSClientBase) expectDescribeInstanceTypeOfferings(instanceType, availabilityZone string,
	retOfferings ...types.InstanceTypeOffering,
) {
	f.awsClient.EXPECT().DescribeInstanceTypeOfferings(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("location"),
		Values: []string{availabilityZone},
	}, types.Filter{
		Name:   awssdk.String("instance-type"),
		Values: []string{instanceType},
	})).Return(&ec2.DescribeInstanceTypeOfferingsOutput{InstanceTypeOfferings: retOfferings}, f.describeInstanceTypeOfferingsErr).AnyTimes()
}

func (f *fakeAWSClientBase) expectCreateTags(subnetID string, tagKeys ...string) {
	f.awsClient.EXPECT().CreateTags(gomock.Any(), mock.Eq(&ec2.CreateTagsInput{
		Resources: []string{subnetID},
		Tags:      makeTags(tagKeys),
	})).Return(&ec2.CreateTagsOutput{}, f.createTagsErr)
}

func (f *fakeAWSClientBase) expectCreateGatewayTags(subnetID string) {
	f.expectCreateTags(subnetID, "kubernetes.io/role/internal-elb", "submariner.io/gateway")
}

func (f *fakeAWSClientBase) expectValidateCreateTags() *gomock.Call {
	return f.awsClient.EXPECT().CreateTags(gomock.Any(), eqDryRun(&ec2.CreateTagsInput{})).Return(&ec2.CreateTagsOutput{}, nil)
}

func (f *fakeAWSClientBase) expectDeleteTags(subnetID string, tagKeys ...string) {
	f.awsClient.EXPECT().DeleteTags(gomock.Any(), mock.Eq(&ec2.DeleteTagsInput{
		Resources: []string{subnetID},
		Tags:      makeTags(tagKeys),
	})).Return(&ec2.DeleteTagsOutput{}, f.createTagsErr)
}

func (f *fakeAWSClientBase) expectDeleteGatewayTags(subnetID string) {
	f.expectDeleteTags(subnetID, "kubernetes.io/role/internal-elb", "submariner.io/gateway")
}

func (f *fakeAWSClientBase) expectValidateDeleteTags() *gomock.Call {
	return f.awsClient.EXPECT().DeleteTags(gomock.Any(), eqDryRun(&ec2.DeleteTagsInput{})).Return(&ec2.DeleteTagsOutput{}, nil)
}

func (f *fakeAWSClientBase) expectDescribeInstances(retImageID string) {
	var reservations []types.Reservation
	if retImageID != "" {
		reservations = []types.Reservation{
			{
				Instances: []types.Instance{
					{
						ImageId: awssdk.String(retImageID),
					},
				},
			},
		}
	}

	f.awsClient.EXPECT().DescribeInstances(gomock.Any(), eqFilters(types.Filter{
		Name:   awssdk.String("tag:Name"),
		Values: []string{infraID + "-worker*"},
	}, types.Filter{
		Name:   awssdk.String("vpc-id"),
		Values: []string{f.vpcID},
	}, types.Filter{
		Name:   awssdk.String(clusterFilterTagName),
		Values: []string{"owned"},
	})).Return(&ec2.DescribeInstancesOutput{Reservations: reservations}, nil).AnyTimes()
}

func makeTags(tagKeys []string) []types.Tag {
	tags := make([]types.Tag, len(tagKeys))
	for i := range tagKeys {
		tags[i] = types.Tag{
			Key:   awssdk.String(tagKeys[i]),
			Value: awssdk.String(""),
		}
	}

	return tags
}

func newSubnet(availabilityZone, subnetID string) types.Subnet {
	return types.Subnet{
		SubnetId:         awssdk.String(subnetID),
		AvailabilityZone: awssdk.String(availabilityZone),
		Tags: []types.Tag{
			{
				Key:   awssdk.String("Name"),
				Value: awssdk.String(subnetName(subnetID)),
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
				Name:   awssdk.String("vpc-id"),
				Values: []string{vpcID},
			},
			{
				Name:   awssdk.String("tag:Name"),
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
			GroupId:       awssdk.String(groupID),
			IpPermissions: ipPermissions,
		},
	}}
}

func newIPPermission(desc string) types.IpPermission {
	return types.IpPermission{
		UserIdGroupPairs: []types.UserIdGroupPair{
			{
				Description: awssdk.String(desc),
			},
		},
	}
}

func newClusterSGRule(groupID string, port int, protocol string) *types.IpPermission {
	return &types.IpPermission{
		FromPort:   awssdk.Int32(int32(port)),
		ToPort:     awssdk.Int32(int32(port)),
		IpProtocol: awssdk.String(protocol),
		UserIdGroupPairs: []types.UserIdGroupPair{
			{
				GroupId:     awssdk.String(groupID),
				Description: awssdk.String(internalTrafficDesc),
			},
		},
	}
}

func newPublicSGRule(port int, protocol string) *types.IpPermission {
	return &types.IpPermission{
		FromPort:   awssdk.Int32(int32(port)),
		ToPort:     awssdk.Int32(int32(port)),
		IpProtocol: awssdk.String(protocol),
		IpRanges: []types.IpRange{
			{
				CidrIp: awssdk.String("0.0.0.0/0"),
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
					out.UserIdGroupPairs[i].Description = awssdk.String(internalTrafficDesc)
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

func (m *authorizeSecurityGroupIngressInputMatcher) String() string {
	return "matches " + mock.FormatToYAML(&m.AuthorizeSecurityGroupIngressInput)
}

func eqAuthorizeSecurityGroupIngressInput(srcGroup string, ipPerm *types.IpPermission) gomock.Matcher {
	m := &authorizeSecurityGroupIngressInputMatcher{
		AuthorizeSecurityGroupIngressInput: ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       awssdk.String(srcGroup),
			IpPermissions: []types.IpPermission{*ipPerm},
		},
	}

	return mock.FormattingMatcher(&m.AuthorizeSecurityGroupIngressInput, m)
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

func (m *filtersMatcher) String() string {
	return "matches filters " + mock.FormatToYAML(m.expectedFilters)
}

func eqFilters(expectedFilters ...types.Filter) gomock.Matcher {
	m := &filtersMatcher{
		expectedFilters: expectedFilters,
	}

	return gomock.GotFormatterAdapter(gomock.GotFormatterFunc(func(o interface{}) string {
		return mock.FormatToYAML(reflect.Indirect(reflect.ValueOf(o)).FieldByName("Filters").Interface())
	}), gomock.WantFormatter(gomock.StringerFunc(func() string {
		return mock.FormatToYAML(m.expectedFilters)
	}), m))
}

type dryRunMatcher struct{}

func (m *dryRunMatcher) Matches(i interface{}) bool {
	dryRun := reflect.Indirect(reflect.ValueOf(i)).FieldByName("DryRun").Interface().(*bool)
	return dryRun != nil && *dryRun
}

func (m *dryRunMatcher) String() string {
	return "is a dry run"
}

func eqDryRun(i interface{}) gomock.Matcher {
	t := true
	dryRun := reflect.Indirect(reflect.ValueOf(i)).FieldByName("DryRun")
	dryRun.Set(reflect.ValueOf(&t))

	return mock.FormattingMatcher(i, &dryRunMatcher{})
}
