package ec2

import (
	"fmt"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2api "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aquasecurity/trivy-aws/internal/adapters/cloud/aws"
	"github.com/aquasecurity/trivy-aws/pkg/concurrency"
	"github.com/aquasecurity/trivy-aws/pkg/types"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/ec2"
	"github.com/aquasecurity/trivy/pkg/iac/state"
	trivyTypes "github.com/aquasecurity/trivy/pkg/iac/types"
)

type adapter struct {
	*aws.RootAdapter
	client *ec2api.Client
}

func init() {
	aws.RegisterServiceAdapter(&adapter{})
}

func (a *adapter) Provider() string {
	return "aws"
}

func (a *adapter) Name() string {
	return "ec2"
}

func (a *adapter) Adapt(root *aws.RootAdapter, state *state.State) error {

	a.RootAdapter = root
	a.client = ec2api.NewFromConfig(root.SessionConfig())
	var err error

	state.AWS.EC2.Instances, err = a.getInstances()
	if err != nil {
		return err
	}

	state.AWS.EC2.SecurityGroups, err = a.getSecurityGroups()
	if err != nil {
		return err
	}

	state.AWS.EC2.NetworkACLs, err = a.getNetworkACLs()
	if err != nil {
		return err
	}

	state.AWS.EC2.VPCs, err = a.getVPCs()
	if err != nil {
		return err
	}

	state.AWS.EC2.LaunchTemplates, err = a.getLaunchTemplates()
	if err != nil {
		return err
	}

	state.AWS.EC2.Volumes, err = a.getVolumes()
	if err != nil {
		return err
	}

	for i, vpc := range state.AWS.EC2.VPCs {
		for _, group := range state.AWS.EC2.SecurityGroups {
			if group.VPCID.EqualTo(vpc.ID.Value()) {
				state.AWS.EC2.VPCs[i].SecurityGroups = append(state.AWS.EC2.VPCs[i].SecurityGroups, group)
			}
		}
	}

	return nil
}

func (a *adapter) getInstances() (instances []ec2.Instance, err error) {

	a.Tracker().SetServiceLabel("Discovering instances...")
	var apiInstances []ec2Types.Instance
	input := &ec2api.DescribeInstancesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   awssdk.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	for {
		output, err := a.client.DescribeInstances(a.Context(), input)
		if err != nil {
			return nil, err
		}
		for _, res := range output.Reservations {
			apiInstances = append(apiInstances, res.Instances...)
		}

		a.Tracker().SetTotalResources(len(apiInstances))
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	a.Tracker().SetServiceLabel("Adapting instances...")
	return concurrency.Adapt(apiInstances, a.RootAdapter, a.adaptInstance), nil
}

func (a *adapter) adaptInstance(instance ec2Types.Instance) (*ec2.Instance, error) {

	volumeBlockMap := make(map[string]*ec2.BlockDevice)
	var volumeIds []string
	instanceMetadata := a.CreateMetadata("instance/" + *instance.InstanceId)

	i := ec2.NewInstance(instanceMetadata)
	if instance.MetadataOptions != nil {
		i.MetadataOptions.HttpTokens = trivyTypes.StringDefault(string(instance.MetadataOptions.HttpTokens), instanceMetadata)
		i.MetadataOptions.HttpEndpoint = trivyTypes.StringDefault(string(instance.MetadataOptions.HttpEndpoint), instanceMetadata)
	}

	if instance.BlockDeviceMappings != nil {
		for _, blockMapping := range instance.BlockDeviceMappings {
			volumeMetadata := a.CreateMetadata(fmt.Sprintf("volume/%s", *blockMapping.Ebs.VolumeId))
			ebsDevice := &ec2.BlockDevice{
				Metadata:  volumeMetadata,
				Encrypted: trivyTypes.BoolDefault(false, volumeMetadata),
			}
			if strings.EqualFold(*blockMapping.DeviceName, *instance.RootDeviceName) {
				// is root block device
				i.RootBlockDevice = ebsDevice
			} else {
				i.EBSBlockDevices = append(i.EBSBlockDevices, ebsDevice)
			}
			volumeBlockMap[*blockMapping.Ebs.VolumeId] = ebsDevice
			volumeIds = append(volumeIds, *blockMapping.Ebs.VolumeId)
		}
	}

	volumes, err := a.client.DescribeVolumes(a.Context(), &ec2api.DescribeVolumesInput{
		VolumeIds: volumeIds,
	})
	if err != nil {
		return nil, err
	}

	for _, v := range volumes.Volumes {
		block := volumeBlockMap[*v.VolumeId]
		if block != nil {
			block.Encrypted = types.ToBool(v.Encrypted, block.Metadata)
		}
	}
	return i, nil
}
