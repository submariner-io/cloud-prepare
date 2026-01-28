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

package client

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

//go:generate mockery

// Interface wraps an actual AWS SDK ec2 client to allow for easier testing.
type Interface interface {
	AuthorizeSecurityGroupIngress(ctx context.Context, params *ec2.AuthorizeSecurityGroupIngressInput,
		optFns ...func(*ec2.Options)) (*ec2.AuthorizeSecurityGroupIngressOutput, error)
	CreateSecurityGroup(ctx context.Context, params *ec2.CreateSecurityGroupInput,
		optFns ...func(*ec2.Options)) (*ec2.CreateSecurityGroupOutput, error)
	CreateTags(ctx context.Context, params *ec2.CreateTagsInput,
		optFns ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput,
		optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeVpcs(ctx context.Context, params *ec2.DescribeVpcsInput,
		optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeSecurityGroups(ctx context.Context, params *ec2.DescribeSecurityGroupsInput,
		optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeSubnets(ctx context.Context, params *ec2.DescribeSubnetsInput,
		optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeInstanceTypeOfferings(ctx context.Context, params *ec2.DescribeInstanceTypeOfferingsInput,
		optFns ...func(*ec2.Options)) (*ec2.DescribeInstanceTypeOfferingsOutput, error)
	DeleteSecurityGroup(ctx context.Context, params *ec2.DeleteSecurityGroupInput,
		optFns ...func(*ec2.Options)) (*ec2.DeleteSecurityGroupOutput, error)
	DeleteTags(ctx context.Context, params *ec2.DeleteTagsInput, optFns ...func(*ec2.Options)) (*ec2.DeleteTagsOutput, error)
	RevokeSecurityGroupIngress(ctx context.Context, params *ec2.RevokeSecurityGroupIngressInput,
		optFns ...func(*ec2.Options)) (*ec2.RevokeSecurityGroupIngressOutput, error)
}

type Option = config.LoadOptionsFunc

func WithCredentials(accessKeyID, secretAccessKey string) Option {
	return config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""))
}

func WithCredentialsFile(file string) Option {
	return config.WithSharedCredentialsFiles([]string{file})
}

func WithConfigProfile(p string) Option {
	return config.WithSharedConfigProfile(p)
}

// DefaultCredentialsFile returns the default credentials file name.
func DefaultCredentialsFile() string {
	return config.DefaultSharedCredentialsFilename()
}

// DefaultProfile returns the default profile name.
func DefaultProfile() string {
	return "default"
}

func New(ctx context.Context, region string, opts ...Option) (Interface, error) {
	options := make([]func(*config.LoadOptions) error, 0, 1+len(opts))
	options = append(options, config.WithRegion(region))

	for _, opt := range opts {
		options = append(options, opt)
	}

	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	return ec2.NewFromConfig(cfg), nil
}
