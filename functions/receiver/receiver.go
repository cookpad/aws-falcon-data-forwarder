package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"

	"github.com/aws/aws-sdk-go/aws/credentials"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/pkg/errors"
)

func main() {
	lambda.Start(handleRequest)
}

type Event struct{}

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

func handleRequest(ctx context.Context, event Event) (string, error) {
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

func ReceiveMessages(sqsURL, awsKey, awsSecret string, msgHandler func(msg *FalconMessage) error) error {
	cred := credentials.NewStaticCredentials(awsKey, awsSecret, "")

	ssn := session.Must(session.NewSessionWithOptions(session.Options{
		Config:            aws.Config{Credentials: cred},
		SharedConfigState: session.SharedConfigEnable,
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

func Handler(args *Args) (resp string, err error) {
	forwardMessage := func(msg *FalconMessage) error {
		return nil
	}

	err = ReceiveMessages(args.SqsURL, args.AwsKey, args.AwsSecret, forwardMessage)
	return
}
