package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"regexp"

	"github.com/aws/aws-sdk-go/aws/credentials"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"
)

func main() {
	lambda.Start(handleRequest)
}

type lambdaEvent struct{}

var falconAwsRegion = "us-west-1"

func decryptKMS(encrypted string) (string, error) {
	ssn := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	// ssn := session.Must(session.NewSession(&aws.Config{
	// Region: aws.String(region),

	encBin, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", errors.Wrap(err, "Fail to decode")
	}

	svc := kms.New(ssn)
	input := &kms.DecryptInput{
		CiphertextBlob: encBin,
	}

	result, err := svc.Decrypt(input)
	if err != nil {
		return "", errors.Wrap(err, "Fail to decrypt")
	}

	return string(result.Plaintext), nil
}

func handleRequest(ctx context.Context, event lambdaEvent) (string, error) {
	args, err := BuildArgs()
	if err != nil {
		return "", err
	}

	return Handler(args)
}

// BuildArgs builds argument of receiver from environment variables.
func BuildArgs() (args *Args, err error) {
	awsKey, err := decryptKMS(os.Getenv("ENC_SQS_AWS_KEY"))
	if err != nil {
		return
	}

	awsSecret, err := decryptKMS(os.Getenv("ENC_SQS_AWS_SECRET"))
	if err != nil {
		return
	}

	args = &Args{
		S3Bucket:  os.Getenv("S3_BUCKET"),
		S3Prefix:  os.Getenv("S3_PREFIX"),
		S3Region:  os.Getenv("S3_REGION"),
		AwsKey:    awsKey,
		AwsSecret: awsSecret,
		SqsURL:    os.Getenv("SQS_URL"),
	}

	return
}

type Args struct {
	S3Bucket  string
	S3Prefix  string
	S3Region  string
	AwsKey    string
	AwsSecret string
	SqsURL    string
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

	ssn := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(sqsRegion),
		Credentials: credentials.NewStaticCredentials(awsKey, awsSecret, ""),
	}))

	queue := sqs.New(ssn)

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

		log.Printf("recv queue, messages = %d", len(result.Messages))

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

func ForwardS3File(awsKey, awsSecret, srcRegion, srcBucket, srcKey, dstRegion, dstBucket, dstKey string) error {
	// Download
	srcSsn := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(srcRegion),
		Credentials: credentials.NewStaticCredentials(awsKey, awsSecret, ""),
	}))
	downSrv := s3.New(srcSsn)
	getInput := &s3.GetObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	}

	getResult, err := downSrv.GetObject(getInput)
	if err != nil {
		return errors.Wrap(err, "Fail to download data from Falcon")
	}

	// Upload
	dstSsn := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(dstRegion),
	}))
	uploader := s3manager.NewUploader(dstSsn)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(dstBucket),
		Key:    aws.String(dstKey),
		Body:   getResult.Body,
	})
	if err != nil {
		return errors.Wrap(err, "Fail to upload data to your bucket")
	}

	return nil
}

func Handler(args *Args) (resp string, err error) {
	forwardMessage := func(msg *FalconMessage) error {
		// log.Printf("message = %v\n", msg)
		for _, f := range msg.Files {
			log.Printf("  forwarding: %v\n", f)
			ForwardS3File(args.AwsKey, args.AwsSecret,
				falconAwsRegion, msg.Bucket, f.Path,
				args.S3Region, args.S3Bucket, args.S3Prefix+f.Path)
		}
		return nil
	}

	err = ReceiveMessages(args.SqsURL, args.AwsKey, args.AwsSecret, forwardMessage)
	return
}
