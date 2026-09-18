package aws

import (
	"context"

	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

func sharedLimitRetryPolicy(ctx context.Context) func(*bedrockruntime.Options) {
	return func(options *bedrockruntime.Options) {
		if service.HasSharedChannelLimitLease(ctx) {
			options.Retryer = aws.NopRetryer{}
		}
	}
}
