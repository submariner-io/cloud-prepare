package aws

import (
	"bytes"
	"text/template"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/dynamic"
)

type machineSetConfig struct {
	AZ            string
	AMIId         string
	InfraID       string
	InstanceType  string
	Region        string
	SecurityGroup string
	PublicSubnet  string
}

func (ac *awsCloud) findAMIID(vpcID string) (string, error) {
	result, err := ac.client.DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			ec2Filter("vpc-id", vpcID),
			ac.filterByName("{infraID}-worker*"),
			ac.filterByCurrentCluster(),
		},
	})

	if err != nil {
		return "", err
	}

	if len(result.Reservations) == 0 {
		return "", newNotFoundError("reservations")
	}
	if len(result.Reservations[0].Instances) == 0 {
		return "", newNotFoundError("worker instances")
	}
	if result.Reservations[0].Instances[0].ImageId == nil {
		return "", newNotFoundError("AMI ID")
	}
	return *result.Reservations[0].Instances[0].ImageId, nil
}

func (ac *awsCloud) loadGatewayYAML(vpcID, gatewaySecurityGroup string, publicSubnet *ec2.Subnet) ([]byte, error) {
	var buf bytes.Buffer

	// TODO: Not working properly, but we should revisit this as it makes more sense
	// tpl, err := template.ParseFiles("pkg/aws/gw-machineset.yaml")
	tpl, err := template.New("").Parse(machineSetYAML)
	if err != nil {
		return nil, err
	}

	amiID, err := ac.findAMIID(vpcID)
	if err != nil {
		return nil, err
	}

	tplVars := machineSetConfig{
		AZ:            *publicSubnet.AvailabilityZone,
		AMIId:         amiID,
		InfraID:       ac.infraID,
		InstanceType:  ac.gwInstanceType,
		Region:        ac.region,
		SecurityGroup: gatewaySecurityGroup,
		PublicSubnet:  extractName(publicSubnet.Tags),
	}

	err = tpl.Execute(&buf, tplVars)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (ac *awsCloud) deployGateway(vpcID, gatewaySecurityGroup string, publicSubnet *ec2.Subnet) error {
	gatewayYAML, err := ac.loadGatewayYAML(vpcID, gatewaySecurityGroup, publicSubnet)
	if err != nil {
		return err
	}

	k8sClient, err := dynamic.NewForConfig(ac.k8sConfig)
	if err != nil {
		return err
	}

	unstructDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	machineSet := &unstructured.Unstructured{}
	_, _, err = unstructDecoder.Decode(gatewayYAML, nil, machineSet)
	if err != nil {
		return err
	}

	restMapper, err := util.BuildRestMapper(ac.k8sConfig)
	if err != nil {
		return err
	}

	machineSet, gvr, err := util.ToUnstructuredResource(machineSet, restMapper)
	if err != nil {
		return err
	}

	dynamicClient := k8sClient.Resource(*gvr).Namespace(machineSet.GetNamespace())
	machineSetClient := resource.ForDynamic(dynamicClient)

	_, err = util.CreateOrUpdate(machineSetClient, machineSet, util.Replace(machineSet))

	return err
}
