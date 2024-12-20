package emr

import (
	api "github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"

	"github.com/aquasecurity/trivy-aws/internal/adapters/cloud/aws"
	"github.com/aquasecurity/trivy-aws/pkg/concurrency"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/emr"
	"github.com/aquasecurity/trivy/pkg/iac/state"
	trivyTypes "github.com/aquasecurity/trivy/pkg/iac/types"
	"github.com/aquasecurity/trivy/pkg/log"
)

type adapter struct {
	*aws.RootAdapter
	api *api.Client
}

func init() {
	aws.RegisterServiceAdapter(&adapter{})
}

func (a *adapter) Provider() string {
	return "aws"
}

func (a *adapter) Name() string {
	return "emr"
}

func (a *adapter) Adapt(root *aws.RootAdapter, state *state.State) error {

	a.RootAdapter = root
	a.api = api.NewFromConfig(root.SessionConfig())
	var err error

	state.AWS.EMR.Clusters, err = a.getClusters()
	if err != nil {
		return err
	}

	state.AWS.EMR.SecurityConfiguration, err = a.getSecurityConfigurations()
	if err != nil {
		return err
	}

	return nil
}

func (a *adapter) getClusters() ([]emr.Cluster, error) {

	a.Tracker().SetServiceLabel("Discovering clusters...")

	var apiClusters []types.ClusterSummary
	var input api.ListClustersInput
	for {
		output, err := a.api.ListClusters(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiClusters = append(apiClusters, output.Clusters...)
		a.Tracker().SetTotalResources(len(apiClusters))
		if output.Marker == nil {
			break
		}
		input.Marker = output.Marker
	}

	a.Tracker().SetServiceLabel("Adapting clusters...")
	return concurrency.Adapt(apiClusters, a.RootAdapter, a.adaptCluster), nil
}

func (a *adapter) adaptCluster(apiCluster types.ClusterSummary) (*emr.Cluster, error) {

	metadata := a.CreateMetadataFromARN(*apiCluster.ClusterArn)

	output, err := a.api.DescribeCluster(a.Context(), &api.DescribeClusterInput{
		ClusterId: apiCluster.Id,
	})
	if err != nil {
		return nil, err
	}

	name := trivyTypes.StringDefault("", metadata)
	if apiCluster.Name != nil {
		name = trivyTypes.String(*apiCluster.Name, metadata)
	}

	releaseLabel := trivyTypes.StringDefault("", metadata)
	if output.Cluster != nil && output.Cluster.ReleaseLabel != nil {
		releaseLabel = trivyTypes.String(*output.Cluster.ReleaseLabel, metadata)
	}

	serviceRole := trivyTypes.StringDefault("", metadata)
	if output.Cluster != nil && output.Cluster.ServiceRole != nil {
		serviceRole = trivyTypes.String(*output.Cluster.ServiceRole, metadata)
	}

	return &emr.Cluster{
		Metadata: metadata,
		Settings: emr.ClusterSettings{
			Metadata:     metadata,
			Name:         name,
			ReleaseLabel: releaseLabel,
			ServiceRole:  serviceRole,
		},
	}, nil
}

func (a *adapter) getSecurityConfigurations() ([]emr.SecurityConfiguration, error) {
	a.Tracker().SetServiceLabel("Discovering security configurations...")

	var apiConfigs []types.SecurityConfigurationSummary
	var input api.ListSecurityConfigurationsInput
	for {
		output, err := a.api.ListSecurityConfigurations(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiConfigs = append(apiConfigs, output.SecurityConfigurations...)
		a.Tracker().SetTotalResources(len(apiConfigs))
		if output.Marker == nil {
			break
		}
		input.Marker = output.Marker
	}

	a.Tracker().SetServiceLabel("Adapting security configurations...")

	var configs []emr.SecurityConfiguration
	for _, apiConfig := range apiConfigs {
		config, err := a.adaptConfig(apiConfig)
		if err != nil {
			a.Logger().Error("Failed to adapt security configuration",
				log.String("name", *apiConfig.Name), log.Err(err))
			continue
		}
		configs = append(configs, *config)
		a.Tracker().IncrementResource()
	}

	return configs, nil
}

func (a *adapter) adaptConfig(config types.SecurityConfigurationSummary) (*emr.SecurityConfiguration, error) {

	metadata := a.CreateMetadata("config/" + *config.Name)

	output, err := a.api.DescribeSecurityConfiguration(a.Context(), &api.DescribeSecurityConfigurationInput{
		Name: config.Name,
	})
	if err != nil {
		return nil, err
	}

	name := trivyTypes.StringDefault("", metadata)
	if config.Name != nil {
		name = trivyTypes.String(*config.Name, metadata)
	}

	secConf := trivyTypes.StringDefault("", metadata)
	if output.SecurityConfiguration != nil {
		secConf = trivyTypes.String(*output.SecurityConfiguration, metadata)
	}

	return &emr.SecurityConfiguration{
		Metadata:      metadata,
		Name:          name,
		Configuration: secConf,
	}, nil
}
