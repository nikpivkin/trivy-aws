package iam

import (
	"fmt"

	iamapi "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/aquasecurity/trivy-aws/pkg/concurrency"
	"github.com/aquasecurity/trivy/pkg/iac/providers/aws/iam"
	"github.com/aquasecurity/trivy/pkg/iac/state"
	trivyTypes "github.com/aquasecurity/trivy/pkg/iac/types"
)

func (a *adapter) adaptServerCertificates(state *state.State) error {
	a.Tracker().SetServiceLabel("Discovering server certificates...")

	var certs []iamtypes.ServerCertificateMetadata

	input := &iamapi.ListServerCertificatesInput{}
	for {
		certsOutput, err := a.api.ListServerCertificates(a.Context(), input)
		if err != nil {
			return err
		}
		certs = append(certs, certsOutput.ServerCertificateMetadataList...)
		a.Tracker().SetTotalResources(len(certs))
		if !certsOutput.IsTruncated {
			break
		}
		input.Marker = certsOutput.Marker
	}

	a.Tracker().SetServiceLabel("Adapting server certificates...")

	state.AWS.IAM.ServerCertificates = concurrency.Adapt(certs, a.RootAdapter, a.adaptServerCertificate)
	return nil
}

func (a *adapter) adaptServerCertificate(certInfo iamtypes.ServerCertificateMetadata) (*iam.ServerCertificate, error) {
	cert, err := a.api.GetServerCertificate(a.Context(), &iamapi.GetServerCertificateInput{
		ServerCertificateName: certInfo.ServerCertificateName,
	})
	if err != nil {
		return nil, err
	}

	if cert.ServerCertificate.ServerCertificateMetadata == nil || cert.ServerCertificate.ServerCertificateMetadata.Arn == nil {
		return nil, fmt.Errorf("server certificate metadata is nil")
	}

	metadata := a.CreateMetadataFromARN(*cert.ServerCertificate.ServerCertificateMetadata.Arn)

	expiration := trivyTypes.TimeUnresolvable(metadata)
	if cert.ServerCertificate.ServerCertificateMetadata.Expiration != nil {
		expiration = trivyTypes.Time(*cert.ServerCertificate.ServerCertificateMetadata.Expiration, metadata)
	}

	return &iam.ServerCertificate{
		Metadata:   metadata,
		Expiration: expiration,
	}, nil
}
