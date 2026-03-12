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

package client_test

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/cloud-prepare/pkg/aws/client"
)

const (
	testRegion          = "us-west-2"
	testAccessKeyID     = "test-access-key"
	testSecretAccessKey = "test-secret-key"
	testProfileName     = "test-profile"
)

var _ = Describe("New", func() {
	It("should return a valid client", func(ctx SpecContext) {
		c, err := client.New(ctx, testRegion)
		Expect(err).NotTo(HaveOccurred())
		Expect(assertEC2Client(c).Options().Region).To(Equal(testRegion))
	})

	Context("WithCredentials", func() {
		It("should obtain the credentials directly from the values provided", func(ctx SpecContext) {
			c, err := client.New(ctx, testRegion, client.WithCredentials(testAccessKeyID, testSecretAccessKey))
			Expect(err).NotTo(HaveOccurred())
			assertCredentials(ctx, assertEC2Client(c))
		})
	})

	Context("WithConfigProfile", func() {
		BeforeEach(func() {
			_ = os.Setenv("AWS_CONFIG_FILE", createTempFile("test-aws-config", fmt.Sprintf("[profile %s]", testProfileName)))

			DeferCleanup(func() {
				_ = os.Unsetenv("AWS_CONFIG_FILE")
				_ = os.Unsetenv("AWS_SHARED_CREDENTIALS_FILE")
			})
		})

		Context("", func() {
			BeforeEach(func() {
				_ = os.Setenv("AWS_SHARED_CREDENTIALS_FILE", createCredentialsFile(testProfileName))
			})

			It("should obtain the credentials defined for the provided profile defined from the default credentials file",
				func(ctx SpecContext) {
					c, err := client.New(ctx, testRegion, client.WithConfigProfile(testProfileName))
					Expect(err).NotTo(HaveOccurred())
					assertCredentials(ctx, assertEC2Client(c))
				})
		})

		Context("and WithCredentialsFile", func() {
			var credentialsFile string

			BeforeEach(func() {
				credentialsFile = createCredentialsFile(testProfileName)
			})

			It("should obtain the credentials defined for the provided profile from the provided credentials file",
				func(ctx SpecContext) {
					c, err := client.New(ctx, testRegion, client.WithConfigProfile(testProfileName),
						client.WithCredentialsFile(credentialsFile))
					Expect(err).NotTo(HaveOccurred())
					assertCredentials(ctx, assertEC2Client(c))
				})
		})
	})

	Context("WithCredentialsFile", func() {
		var credentialsFile string

		BeforeEach(func() {
			credentialsFile = createCredentialsFile(client.DefaultProfile())
		})

		It("should obtain the credentials defined for the default profile from the provided credentials file",
			func(ctx SpecContext) {
				c, err := client.New(ctx, testRegion, client.WithCredentialsFile(credentialsFile))
				Expect(err).NotTo(HaveOccurred())
				assertCredentials(ctx, assertEC2Client(c))
			})
	})

	Context("with a non-existent profile", func() {
		It("should return an error", func(ctx SpecContext) {
			_, err := client.New(ctx, testRegion, client.WithConfigProfile("non-existent"))
			Expect(err).To(HaveOccurred())
		})
	})
})

func assertEC2Client(c client.Interface) *ec2.Client {
	Expect(c).NotTo(BeNil())
	Expect(c).To(BeAssignableToTypeOf(&ec2.Client{}))

	return c.(*ec2.Client)
}

func assertCredentials(ctx context.Context, c *ec2.Client) {
	provider := c.Options().Credentials
	Expect(provider).NotTo(BeNil())
	creds, err := provider.Retrieve(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(creds.AccessKeyID).To(Equal(testAccessKeyID))
	Expect(creds.SecretAccessKey).To(Equal(testSecretAccessKey))
}

func createTempFile(name, data string) string {
	file, err := os.CreateTemp("", name)
	Expect(err).NotTo(HaveOccurred())

	defer file.Close()

	_, err = file.WriteString(data)
	Expect(err).NotTo(HaveOccurred())

	DeferCleanup(func() {
		_ = os.Remove(file.Name())
	})

	return file.Name()
}

func createCredentialsFile(profile string) string {
	//nolint:gosec // Hardcoded credentials aren't real.
	const credentialsFileFmt = `
[%s]
aws_access_key_id = %s
aws_secret_access_key = %s
`

	return createTempFile("test-aws-creds", fmt.Sprintf(credentialsFileFmt, profile, testAccessKeyID, testSecretAccessKey))
}
