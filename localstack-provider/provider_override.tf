# Override file for LocalStack + Moto testing
# This file is copied into the module directory during integration tests
# to configure the AWS provider to use mock endpoints
#
# LocalStack (port 4566): S3, Route53, STS, IAM, DynamoDB, ACM
# Moto (port 5000): CloudFront

# Set CloudFront endpoint for AWS CLI commands (used by cache invalidation)
variable "distribution_cloudfront_endpoint_url" {
  default = "http://moto:5000"
}

provider "aws" {
  region                      = var.aws_provider.region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  endpoints {
    # LocalStack services (using Docker service name)
    s3              = "http://localstack:4566"
    route53         = "http://localstack:4566"
    sts             = "http://localstack:4566"
    iam             = "http://localstack:4566"
    dynamodb        = "http://localstack:4566"
    acm             = "http://localstack:4566"
    # Moto services (CloudFront not in LocalStack free tier)
    cloudfront      = "http://moto:5000"
  }

  default_tags {
    tags = var.provider_resource_tags_json
  }

  s3_use_path_style = true
}
