package elasticache

import (
	api "github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"github.com/aquasecurity/trivy-aws/internal/adapters/cloud/aws"
	"github.com/aquasecurity/trivy-aws/pkg/concurrency"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/elasticache"
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
	return "elasticache"
}

func (a *adapter) Adapt(root *aws.RootAdapter, state *state.State) error {

	a.RootAdapter = root
	a.api = api.NewFromConfig(root.SessionConfig())
	var err error

	state.AWS.ElastiCache.Clusters, err = a.getClusters()
	if err != nil {
		return err
	}

	state.AWS.ElastiCache.ReplicationGroups, err = a.getReplicationGroups()
	if err != nil {
		return err
	}

	// this can error if classic resources are requested where not available
	state.AWS.ElastiCache.SecurityGroups, _ = a.getSecurityGroups()

	return nil
}

func (a *adapter) getClusters() ([]elasticache.Cluster, error) {

	a.Tracker().SetServiceLabel("Discovering clusters...")

	var input api.DescribeCacheClustersInput
	var apiClusters []types.CacheCluster
	for {
		output, err := a.api.DescribeCacheClusters(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiClusters = append(apiClusters, output.CacheClusters...)
		a.Tracker().SetTotalResources(len(apiClusters))
		if output.Marker == nil {
			break
		}
		input.Marker = output.Marker
	}

	a.Tracker().SetServiceLabel("Adapting clusters...")
	return concurrency.Adapt(apiClusters, a.RootAdapter, a.adaptCluster), nil
}

func (a *adapter) adaptCluster(apiCluster types.CacheCluster) (*elasticache.Cluster, error) {
	metadata := a.CreateMetadataFromARN(*apiCluster.ARN)

	engine := trivyTypes.StringDefault("", metadata)
	if apiCluster.Engine != nil {
		engine = trivyTypes.String(*apiCluster.Engine, metadata)
	}

	nodeType := trivyTypes.StringDefault("", metadata)
	if apiCluster.CacheNodeType != nil {
		nodeType = trivyTypes.String(*apiCluster.CacheNodeType, metadata)
	}

	limit := trivyTypes.IntDefault(0, metadata)
	if apiCluster.SnapshotRetentionLimit != nil {
		limit = trivyTypes.Int(int(*apiCluster.SnapshotRetentionLimit), metadata)
	}

	return &elasticache.Cluster{
		Metadata:               metadata,
		Engine:                 engine,
		NodeType:               nodeType,
		SnapshotRetentionLimit: limit,
	}, nil
}

func (a *adapter) getReplicationGroups() ([]elasticache.ReplicationGroup, error) {

	a.Tracker().SetServiceLabel("Discovering replication groups...")

	var input api.DescribeReplicationGroupsInput
	var apiGroups []types.ReplicationGroup
	for {
		output, err := a.api.DescribeReplicationGroups(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiGroups = append(apiGroups, output.ReplicationGroups...)
		a.Tracker().SetTotalResources(len(apiGroups))
		if output.Marker == nil {
			break
		}
		input.Marker = output.Marker
	}

	a.Tracker().SetServiceLabel("Adapting replication groups...")

	var groups []elasticache.ReplicationGroup
	for _, apiGroup := range apiGroups {
		group, err := a.adaptReplicationGroup(apiGroup)
		if err != nil {
			a.Logger().Error("Failed to adapt replication group", log.String("ARN", *apiGroup.ARN), log.Err(err))
			continue
		}
		groups = append(groups, *group)
		a.Tracker().IncrementResource()
	}

	return groups, nil
}

func (a *adapter) adaptReplicationGroup(apiGroup types.ReplicationGroup) (*elasticache.ReplicationGroup, error) {
	metadata := a.CreateMetadataFromARN(*apiGroup.ARN)

	transitEncrypted := trivyTypes.BoolDefault(false, metadata)
	if apiGroup.TransitEncryptionEnabled != nil {
		transitEncrypted = trivyTypes.Bool(*apiGroup.TransitEncryptionEnabled, metadata)
	}
	atRestEncrypted := trivyTypes.BoolDefault(false, metadata)
	if apiGroup.AtRestEncryptionEnabled != nil {
		atRestEncrypted = trivyTypes.Bool(*apiGroup.AtRestEncryptionEnabled, metadata)
	}

	return &elasticache.ReplicationGroup{
		Metadata:                 metadata,
		TransitEncryptionEnabled: transitEncrypted,
		AtRestEncryptionEnabled:  atRestEncrypted,
	}, nil
}

func (a *adapter) getSecurityGroups() ([]elasticache.SecurityGroup, error) {

	a.Tracker().SetServiceLabel("Discovering security groups...")

	var input api.DescribeCacheSecurityGroupsInput
	var apiGroups []types.CacheSecurityGroup
	for {
		output, err := a.api.DescribeCacheSecurityGroups(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiGroups = append(apiGroups, output.CacheSecurityGroups...)
		a.Tracker().SetTotalResources(len(apiGroups))
		if output.Marker == nil {
			break
		}
		input.Marker = output.Marker
	}

	a.Tracker().SetServiceLabel("Adapting security groups...")

	var groups []elasticache.SecurityGroup
	for _, apiGroup := range apiGroups {
		group, err := a.adaptSecurityGroup(apiGroup)
		if err != nil {
			a.Logger().Error("Failed to adapt security group", log.String("ARN", *apiGroup.ARN), log.Err(err))
			continue
		}
		groups = append(groups, *group)
		a.Tracker().IncrementResource()
	}

	return groups, nil
}

func (a *adapter) adaptSecurityGroup(apiGroup types.CacheSecurityGroup) (*elasticache.SecurityGroup, error) {
	metadata := a.CreateMetadataFromARN(*apiGroup.ARN)
	description := trivyTypes.StringDefault("", metadata)
	if apiGroup.Description != nil {
		description = trivyTypes.String(*apiGroup.Description, metadata)
	}
	return &elasticache.SecurityGroup{
		Metadata:    metadata,
		Description: description,
	}, nil
}
