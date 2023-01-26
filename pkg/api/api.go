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

package api

import "github.com/submariner-io/admiral/pkg/reporter"

// PortSpec is a specification of port+protocol to open.
type PortSpec struct {
	Port     uint16
	Protocol string
}

// Cloud is a potential cloud for installing Submariner on.
type Cloud interface {
	// OpenPorts inside the cloud for submariner to communicate through.
	OpenPorts(ports []PortSpec, status reporter.Interface) error

	// ClosePorts will close any internal ports that were opened, after Submariner is removed.
	ClosePorts(status reporter.Interface) error
}

type GatewayDeployInput struct {
	// List of ports to open externally so that Submariner can reach and be reached by other Submariners.
	PublicPorts []PortSpec

	// Amount of gateways that are being deployed.
	//
	// 0 = Deploy gateways per the default deployer policy (Default if not specified)
	//
	// 1-* = Deploy the amount of gateways requested (May fail if there aren't enough public subnets)
	Gateways int

	// Use service of type LoadBalancer to deploy Submariner
	UseLoadBalancer bool
}

// GatewayDeployer will deploy and cleanup dedicated gateways according to the requested policy.
type GatewayDeployer interface {
	// Deploy dedicated gateways as requested.
	Deploy(input GatewayDeployInput, status reporter.Interface) error

	// Cleanup any dedicated gateways that were previously deployed.
	Cleanup(status reporter.Interface) error
}
