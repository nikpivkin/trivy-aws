package ecr

import (
	ecrapi "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/aquasecurity/iamgo"
	"github.com/aquasecurity/trivy-aws/internal/adapters/cloud/aws"
	"github.com/aquasecurity/trivy-aws/pkg/concurrency"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/ecr"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/iam"
	"github.com/aquasecurity/trivy/pkg/iac/state"
	trivyTypes "github.com/aquasecurity/trivy/pkg/iac/types"
)

type adapter struct {
	*aws.RootAdapter
	api *ecrapi.Client
}

func init() {
	aws.RegisterServiceAdapter(&adapter{})
}

func (a *adapter) Provider() string {
	return "aws"
}

func (a *adapter) Name() string {
	return "ecr"
}

func (a *adapter) Adapt(root *aws.RootAdapter, state *state.State) error {

	a.RootAdapter = root
	a.api = ecrapi.NewFromConfig(root.SessionConfig())
	var err error

	state.AWS.ECR.Repositories, err = a.getRepositories()
	if err != nil {
		return err
	}

	return nil
}

func (a *adapter) getRepositories() ([]ecr.Repository, error) {

	a.Tracker().SetServiceLabel("Discovering repositories...")

	var input ecrapi.DescribeRepositoriesInput

	var apiRepositories []types.Repository
	for {
		output, err := a.api.DescribeRepositories(a.Context(), &input)
		if err != nil {
			return nil, err
		}
		apiRepositories = append(apiRepositories, output.Repositories...)
		a.Tracker().SetTotalResources(len(apiRepositories))
		if output.NextToken == nil {
			break
		}
		input.NextToken = output.NextToken
	}

	a.Tracker().SetServiceLabel("Adapting repositories...")
	return concurrency.Adapt(apiRepositories, a.RootAdapter, a.adaptRepository), nil
}

func (a *adapter) adaptRepository(apiRepository types.Repository) (*ecr.Repository, error) {

	metadata := a.CreateMetadataFromARN(*apiRepository.RepositoryArn)

	var encType string
	var encKey string
	if apiRepository.EncryptionConfiguration != nil {
		encType = string(apiRepository.EncryptionConfiguration.EncryptionType)
		if apiRepository.EncryptionConfiguration.KmsKey != nil {
			encKey = *apiRepository.EncryptionConfiguration.KmsKey
		}
	}

	immutable := apiRepository.ImageTagMutability == types.ImageTagMutabilityImmutable
	scanOnPush := apiRepository.ImageScanningConfiguration != nil && apiRepository.ImageScanningConfiguration.ScanOnPush

	var policies []iam.Policy

	if output, err := a.api.GetRepositoryPolicy(a.Context(), &ecrapi.GetRepositoryPolicyInput{
		RepositoryName: apiRepository.RepositoryName,
	}); err == nil {
		parsed, err := iamgo.ParseString(*output.PolicyText)
		if err != nil {
			return nil, err
		}
		name := trivyTypes.StringDefault("", metadata)
		if output.RepositoryName != nil {
			name = trivyTypes.String(*output.RepositoryName, metadata)
		}
		policies = append(policies, iam.Policy{
			Metadata: metadata,
			Name:     name,
			Document: iam.Document{
				Metadata: metadata,
				Parsed:   *parsed,
			},
			Builtin: trivyTypes.Bool(false, metadata),
		})
	}

	return &ecr.Repository{
		Metadata: metadata,
		ImageScanning: ecr.ImageScanning{
			Metadata:   metadata,
			ScanOnPush: trivyTypes.Bool(scanOnPush, metadata),
		},
		ImageTagsImmutable: trivyTypes.Bool(immutable, metadata),
		Policies:           policies,
		Encryption: ecr.Encryption{
			Metadata: metadata,
			Type:     trivyTypes.String(encType, metadata),
			KMSKeyID: trivyTypes.String(encKey, metadata),
		},
	}, nil
}
