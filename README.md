# aws-falcon-data-forwarder

## What is this

This lambda function receives SQS message(s) from Data Replicator of CrowdStrike Falcon and transfer log files to your own S3 bucket. This service is deployed as AWS CloudFormation (CFn) stack with SAM technology.

## Architecture

![aws-falcon-data-forwarder-arch](https://user-images.githubusercontent.com/605953/43566627-0bc5ce66-966a-11e8-8e04-3c7a24b123b7.png)

## Prerequisite

- Tools
  - go >= 1.10.3
  - mage https://github.com/magefile/mage
  - dep https://github.com/golang/dep
  - aws-cli https://github.com/aws/aws-cli
- Your AWS resources
  - AWS Credential for CLI (like `~/.aws/credentials` )
  - S3 bucket for log data (e.g. `my-log-bucket` )
  - S3 bucket for lambda function code (e.g. `my-function-code` )
  - KMS key (e.g. `arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx` )
  - IAM role for Lambda function (e.g. `arn:aws:iam::1234567890:role/LambdaFalconDataForwarder`)
    - s3::PutObject for `my-log-bucket` 
    - kms:Decrypt for `arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx`

Make sure that you need CrowdStrike Falcon and Data Replicator service.

## Setup

### Encrypt AWS API Key and Secret

You need to encrypt AWS API Key (AWS_ACCESS_KEY_ID) and Secret (AWS_SECRET_ACCESS_KEY) provided CrowdStrike Falcon. Assuming AWS_ACCESS_KEY_ID is `ABCDEFG` and AWS_ACCESS_KEY_ID is `STUVWXYZ`.

```sh
$ aws kms encrypt --key-id arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx --plaintext 'ABCDEFG'
{
    "CiphertextBlob": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX==",
    "KeyId": "arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx"
}
$ aws kms encrypt --key-id arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx --plaintext 'STUVWXYZ'
{
    "CiphertextBlob": "YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY==",
    "KeyId": "arn:aws:kms:ap-northeast-1:1234567890:key/e35cda0e-xxxx-xxxx-xxxx-xxxxxxxxxxxxx"
}
```

Copy text in `CiphertextBlob`.

### Configure

Prepare a configuration file. (e.g. `myconfig.cfg` ) Please see a following sample.

```conf
StackName=falcon-data-forwarder-staging
CodeS3Bucket=my-function-code
CodeS3Prefix=functions
CodeS3Region=ap-northeast-1

RoleArn=arn:aws:iam::1234567890:role/LambdaFalconDataForwarder
S3Bucket=my-log-bucket
S3Prefix=logs/
S3Region=ap-northeast-1
SqsURL=https://us-west-1.queue.amazonaws.com/xxxxxxxxxxxxxx/some-queue-name
EncSqsAwsKey=XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX==
EncSqsAwsSecret=YYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY==
```

- Management
  - `StackName`: CloudFormation(CFn) stack name
  - `CodeS3Bucket`: S3 bucket name to save binary for lambda function
  - `CodeS3Prefix`: Prefix of S3 Key to save binary for lambda function- 
  - `CodeS3Region`: AWS region of `CodeS3Bucket`
- Parameters
  - `RoleArn`: IAM Role ARN for Lambda function
  - `S3Bucket`: S3 Bucket name to save log data
  - `S3Prefix`: Prefix of S3 Key to save log data
  - `S3Regio`: AWS region of `S3Bucket`
  - `SqsURL`: SQS URL provided by CrowdStrike Falcon
  - `EncSqsAwsKey`: Encrypted AWS API Key provided by CrowdStrike Falcon
  - `EncSqsAwsSecret`: Encrypted AWS Secret Key provided by CrowdStrike Falcon

### Deploy

```
$ env PARAM_FILE=myconfig.cfg mage -v deploy
Running target: Deploy
Bulding  receiver
[config/staging.cfg] Packaging...
[config/staging.cfg] Generated template file: /var/folders/3_/nv_wpjw173vgvd3ct4vzjp2r0000gp/T/slam_template_414805156
[config/staging.cfg] Deploy...
[config/staging.cfg] Done!
```
