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

package generic

import (
	"fmt"

	"github.com/submariner-io/cloud-prepare/pkg/api"
	"github.com/submariner-io/cloud-prepare/pkg/k8s"
	v1 "k8s.io/api/core/v1"
)

type gatewayDeployer struct {
	k8sClient k8s.K8sInterface
}

// NewGatewayDeployer created a GatewayDeployer capable of deploying gateways to GCP
func NewGatewayDeployer(k8sClient k8s.K8sInterface) (api.GatewayDeployer, error) {
	return &gatewayDeployer{k8sClient: k8sClient}, nil
}

func (g *gatewayDeployer) Deploy(input api.GatewayDeployInput, reporter api.Reporter) error {
	gwNodes, err := g.k8sClient.ListGatewayNodes()
	if err != nil {
		reporter.Failed(err)
		return err
	}

	// Currently, we only support increasing the number of Gateway nodes which could be a valid use-case
	// to convert a non-HA deployment to an HA deployment. We are not supporting decreasing the Gateway
	// nodes (for now) as it might impact the datapath if we accidentally delete the active GW node.
	if len(gwNodes.Items) >= input.Gateways {
		return nil
	}

	gatewayNodesToDeploy := input.Gateways - len(gwNodes.Items)

	nonGWNodes, err := g.k8sClient.ListNodesWithLabel("!submariner.io/gateway")
	if err != nil {
		reporter.Failed(err)
		return err
	}

	for _, node := range nonGWNodes.Items {
		if isMasterNode(node) {
			// Skip master nodes
			continue
		}

		err = g.k8sClient.AddGWLabelOnNode(node.Name)
		if err != nil {
			reporter.Failed(err)
			return err
		}

		gatewayNodesToDeploy--

		if gatewayNodesToDeploy <= 0 {
			reporter.Succeeded("Successfully deployed gateway nodes")
			return nil
		}
	}

	if gatewayNodesToDeploy > 0 {
		reporter.Failed(fmt.Errorf("there are insufficient number of worker nodes to label as gateways"))
	}

	return nil
}

func (g *gatewayDeployer) Cleanup(reporter api.Reporter) error {
	err := g.k8sClient.RemoveGWLabelFromWorkerNodes()
	if err != nil {
		reporter.Failed(err)
		return err
	}

	reporter.Succeeded("Successfully removed Submariner gateway label from worker nodes")

	return nil
}

func isMasterNode(node v1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node-role.kubernetes.io/master" && taint.Effect == v1.TaintEffectNoSchedule {
			return true
		}
	}

	return false
}
