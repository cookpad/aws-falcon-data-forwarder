package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/credentials"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"

	"github.com/sirupsen/logrus"

	_ "github.com/GoogleCloudPlatform/berglas/pkg/auto"
)

var logger = logrus.New()

func main() {
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)
	lambda.Start(handleRequest)
}

func handleRequest(ctx context.Context, event struct{}) error {
	args, err := BuildArgs()
	if err != nil {
		return err
	}

	return Handler(args)
}

type awsCredential struct {
	key    string
	secret string
}

type S3Ptr struct {
	Region     string
	Bucket     string
	Key        string
	credential *awsCredential
}

func Handler(args Args) error {
	forwardMessage := func(msg *FalconMessage) error {
		t := time.Unix(int64(msg.Timestamp/1000), 0)

		for _, f := range msg.Files {
			logger.WithField("f", f).Info("forwarding")

			src := S3Ptr{
				Region: falconAwsRegion,
				Bucket: msg.Bucket,
				Key:    f.Path,
				credential: &awsCredential{
					key:    args.FalconAwsKey,
					secret: args.FalconAwsSecret,
				},
			}
			dst := S3Ptr{
				Region: args.S3Region,
				Bucket: args.S3Bucket,
				Key: strings.Join([]string{
					args.S3Prefix,
					t.Format("2006/01/02/15/"),
					f.Path,
				}, ""),
			}

			err := ForwardS3File(src, dst)
			if err != nil {
				return err
			}
		}
		return nil
	}

	err := ReceiveMessages(args.SqsURL, args.FalconAwsKey, args.FalconAwsSecret,
		forwardMessage)
	return err
}

var falconAwsRegion = "us-west-1"

func getSecretValues(secretArn string, values interface{}) error {
	// sample: arn:aws:secretsmanager:ap-northeast-1:1234567890:secret:mytest
	arn := strings.Split(secretArn, ":")
	if len(arn) != 7 {
		return errors.New(fmt.Sprintf("Invalid SecretsManager ARN format: %s", secretArn))
	}
	region := arn[3]

	ssn := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))
	mgr := secretsmanager.New(ssn)

	result, err := mgr.GetSecretValue(&secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretArn),
	})

	if err != nil {
		return errors.Wrap(err, "Fail to retrieve secret values")
	}

	err = json.Unmarshal([]byte(*result.SecretString), values)
	if err != nil {
		return errors.Wrap(err, "Fail to parse secret values as JSON")
	}

	return nil
}

// BuildArgs builds argument of receiver from environment variables.
func BuildArgs() (Args, error) {
	return Args{
		S3Bucket:        os.Getenv("S3_BUCKET"),
		S3Prefix:        os.Getenv("S3_PREFIX"),
		S3Region:        os.Getenv("S3_REGION"),
		SqsURL:          os.Getenv("SQS_URL"),
		FalconAwsKey:    os.Getenv("FALCON_AWS_KEY"),
		FalconAwsSecret: os.Getenv("FALCON_AWS_SECRET"), // Get value from Secret Manager via berglas
	}, nil
}

type Args struct {
	S3Bucket        string
	S3Prefix        string
	S3Region        string
	SqsURL          string
	FalconAwsKey    string `json:"falcon_aws_key"`
	FalconAwsSecret string `json:"falcon_aws_secret"`
}

type FalconMessage struct {
	CID        string           `json:"cid"`
	Timestamp  uint             `json:"timestamp"`
	FileCount  int              `json:"fileCount"`
	TotalSize  int              `json:"totalSize"`
	Bucket     string           `json:"bucket"`
	PathPrefix string           `json:"pathPrefix"`
	Files      []FalconLogFiles `json:"files"`
}

type FalconLogFiles struct {
	Path     string `json:"path"`
	Size     int    `json:"size"`
	CheckSum string `json:"checksum"`
}

func sqsURLtoRegion(url string) (string, error) {
	urlPattern := []string{
		// https://sqs.ap-northeast-1.amazonaws.com/21xxxxxxxxxxx/test-queue
		`https://sqs\.([a-z0-9\-]+)\.amazonaws\.com`,

		// https://us-west-1.queue.amazonaws.com/2xxxxxxxxxx/test-queue
		`https://([a-z0-9\-]+)\.queue\.amazonaws\.com`,
	}

	for _, ptn := range urlPattern {
		re := regexp.MustCompile(ptn)
		group := re.FindSubmatch([]byte(url))
		if len(group) == 2 {
			return string(group[1]), nil
		}
	}

	return "", errors.New("unsupported SQS URL syntax")
}

// ReceiveMessages receives SQS message from Falcon side and invokes msgHandler per message.
// In this method, not use channel because SQS queue deletion must be after handling messages
// to keep idempotence.
func ReceiveMessages(sqsURL, awsKey, awsSecret string, msgHandler func(msg *FalconMessage) error) error {

	sqsRegion, err := sqsURLtoRegion(sqsURL)
	if err != nil {
		return err
	}

	cfg := aws.Config{Region: aws.String(sqsRegion)}
	if awsKey != "" && awsSecret != "" {
		cfg.Credentials = credentials.NewStaticCredentials(awsKey, awsSecret, "")
	} else {
		logger.Warn("AWS Key and secret are not set, use role permission")
	}

	queue := sqs.New(session.Must(session.NewSession(&cfg)))

	for {
		result, err := queue.ReceiveMessage(&sqs.ReceiveMessageInput{
			AttributeNames: []*string{
				aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
			},
			MessageAttributeNames: []*string{
				aws.String(sqs.QueueAttributeNameAll),
			},
			QueueUrl:            &sqsURL,
			MaxNumberOfMessages: aws.Int64(1),
			VisibilityTimeout:   aws.Int64(36000), // 10 hours
			WaitTimeSeconds:     aws.Int64(0),
		})

		if err != nil {
			return errors.Wrap(err, "SQS recv error")
		}

		logger.WithField("result", result).Info("recv queue")

		if len(result.Messages) == 0 {
			break
		}

		for _, msg := range result.Messages {
			fmsg := FalconMessage{}
			err = json.Unmarshal([]byte(*msg.Body), &fmsg)
			if err != nil {
				return errors.Wrap(err, "Fail to parse Falcon SNS error")
			}

			if err = msgHandler(&fmsg); err != nil {
				return err
			}
		}

		_, err = queue.DeleteMessage(&sqs.DeleteMessageInput{
			QueueUrl:      &sqsURL,
			ReceiptHandle: result.Messages[0].ReceiptHandle,
		})

		if err != nil {
			return errors.Wrap(err, "SQS queue delete error")
		}
	}

	return nil
}

func ForwardS3File(src, dst S3Ptr) error {
	cfg := aws.Config{Region: aws.String(src.Region)}
	if src.credential != nil {
		cfg.Credentials = credentials.NewStaticCredentials(src.credential.key,
			src.credential.secret, "")

	} else {
		logger.Warn("AWS Key and secret are not set, use role permission")
	}

	// Download
	downSrv := s3.New(session.Must(session.NewSession(&cfg)))
	getInput := &s3.GetObjectInput{
		Bucket: aws.String(src.Bucket),
		Key:    aws.String(src.Key),
	}

	getResult, err := downSrv.GetObject(getInput)
	if err != nil {
		return errors.Wrap(err, "Fail to download data from Falcon")
	}

	// Upload
	dstSsn := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(dst.Region),
	}))
	uploader := s3manager.NewUploader(dstSsn)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(dst.Bucket),
		Key:    aws.String(dst.Key),
		Body:   getResult.Body,
	})
	if err != nil {
		return errors.Wrap(err, "Fail to upload data to your bucket")
	}

	return nil
}
