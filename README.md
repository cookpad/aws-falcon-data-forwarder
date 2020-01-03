# aws-falcon-data-forwarder

## What is this

This lambda function receives SQS message(s) from Data Replicator of CrowdStrike Falcon and transfer log files to your own S3 bucket. This service is deployed as AWS CloudFormation (CFn) stack with SAM technology.

## Architecture

![aws-falcon-data-forwarder-arch](https://user-images.githubusercontent.com/605953/43566627-0bc5ce66-966a-11e8-8e04-3c7a24b123b7.png)

## Prerequisite

- Tools
  - go >= 1.11
  - aws-cli https://github.com/aws/aws-cli
- Your AWS resources
  - AWS Credential for CLI (like `~/.aws/credentials` )
  - S3 bucket for log data (e.g. `my-log-bucket` )
  - S3 bucket for lambda function code (e.g. `my-function-code` )
  - Secrets of Secrets Manager to store AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY for data replicator.
  - IAM role for Lambda function (e.g. `arn:aws:iam::1234567890:role/LambdaFalconDataForwarder`)
    - s3::PutObject for `my-log-bucket`
    - secretsmanager:GetSecretValue

Make sure that you need CrowdStrike Falcon and Data Replicator service.

## Setup

### Setting up AWS Secrets Manager

You need to put AWS API Key (AWS_ACCESS_KEY_ID) and Secret (AWS_SECRET_ACCESS_KEY) provided by CrowdStrike Falcon as secrets of Secrets Manager. Assuming AWS_ACCESS_KEY_ID is `ABCDEFG` and AWS_ACCESS_KEY_ID is `STUVWXYZ`. You can set up the secret by [AWS web console](https://ap-northeast-1.console.aws.amazon.com/secretsmanager).

You need to create 2 items in the secret.

- `falcon_aws_key`: set AWS_ACCESS_KEY_ID provided by CrowdStrike Falcon
- `falcon_aws_secret`: set AWS_SECRET_ACCESS_KEY provided by CrowdStrike Falcon

### Configure

Prepare a configuration file. (e.g. `myconfig.json` ) Please see a following sample.

```json
{
    "StackName": "falcon-data-forwarder-staging",
    "Region": "ap-northeast-1",
    "CodeS3Bucket": "my-function-code",
    "CodeS3Prefix": "functions",

    "RoleArn": "arn:aws:iam::1234567890:role/LambdaFalconDataForwarder",
    "S3Bucket": "my-log-bucket",
    "S3Prefix": "logs/",
    "S3Region": "ap-northeast-1",
    "SqsURL": "https://us-west-1.queue.amazonaws.com/xxxxxxxxxxxxxx/some-queue-name",
    "SecretArn": "arn:aws:secretsmanager:ap-northeast-1:1234567890:secret:your-secret-name-4UqOs6"
}
```

- Management
  - `StackName`: CloudFormation(CFn) stack name
  - `Region`: AWS region where you want to deploy the stack
  - `CodeS3Bucket`: S3 bucket name to save binary for lambda function
  - `CodeS3Prefix`: Prefix of S3 Key to save binary for lambda function
- Parameters
  - `RoleArn`: IAM Role ARN for Lambda function
  - `S3Bucket`: S3 Bucket name to save log data
  - `S3Prefix`: Prefix of S3 Key to save log data
  - `S3Regio`: AWS region of `S3Bucket`
  - `SqsURL`: SQS URL provided by CrowdStrike Falcon
  - `SecretArn`: ARN of the secret that you store AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY

### Deploy

```bash
$ env FORWARDER_CONFIG=myconfig.cfg make deploy
```

## License

MIT License

