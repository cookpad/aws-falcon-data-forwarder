package main_test

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sqs"
	receiver "github.com/m-mizutani/aws-falcon-data-forwarder/functions/receiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Config struct {
	S3Bucket        string
	S3Prefix        string
	S3Region        string
	SqsURL          string
	EncSqsAwsKey    string
	EncSqsAwsSecret string
}

func loadConfig() Config {
	cwd := os.Getenv("PWD")
	var fp *os.File
	var err error

	for cwd != "/" {
		cfgPath := filepath.Join(cwd, "test.json")

		cwd, _ = filepath.Split(strings.TrimRight(cwd, string(filepath.Separator)))

		fp, err = os.Open(cfgPath)
		if err == nil {
			break
		}
	}

	if fp == nil {
		log.Fatal("test.json is not found")
	}

	rawData, err := ioutil.ReadAll(fp)
	if err != nil {
		panic(err)
	}

	cfg := Config{}
	err = json.Unmarshal(rawData, &cfg)
	return cfg
}

func TestBuildConfig(t *testing.T) {
	// Mainly test to decrypt key
	cfg := loadConfig()
	os.Setenv("ENC_SQS_AWS_KEY", cfg.EncSqsAwsKey)
	os.Setenv("ENC_SQS_AWS_SECRET", cfg.EncSqsAwsSecret)
	defer os.Unsetenv("ENC_SQS_AWS_KEY")
	defer os.Unsetenv("ENC_SQS_AWS_SECRET")

	args, err := receiver.BuildArgs()

	assert.NoError(t, err)
	assert.NotEqual(t, "", args.AwsKey)
	assert.NotEqual(t, 0, len(args.AwsKey))
	assert.NotEqual(t, "", args.AwsSecret)
	assert.NotEqual(t, 0, len(args.AwsSecret))
}

func TestHandler(t *testing.T) {
	cfg := loadConfig()

	os.Setenv("ENC_SQS_AWS_KEY", cfg.EncSqsAwsKey)
	os.Setenv("ENC_SQS_AWS_SECRET", cfg.EncSqsAwsSecret)
	os.Setenv("SQS_URL", cfg.SqsURL)
	defer os.Unsetenv("ENC_SQS_AWS_KEY")
	defer os.Unsetenv("ENC_SQS_AWS_SECRET")

	args, err := receiver.BuildArgs()
	require.NoError(t, err)

	result, err := receiver.Handler(args)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestReceiver(t *testing.T) {
	cfg := loadConfig()
	dataKey := "data/test_data.gz"

	sampleMessage := `{
		"cid": "abcdefghijklmn0123456789",
		"timestamp": 1492726639137,
		"fileCount": 4,
		"totalSize": 349986220,
		"bucket": "` + cfg.S3Bucket + `",
		"pathPrefix": "` + cfg.S3Prefix + `",
		"files": [
		  {
			"path": "` + cfg.S3Prefix + dataKey + `",
			"size": 89118480,
			"checksum": "d0f566f37295e46f28c75f71ddce9422"
		  }
		]
	  }`

	// Push test message.
	ssn := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	queue := sqs.New(ssn)
	_, err := queue.SendMessage(&sqs.SendMessageInput{
		DelaySeconds: aws.Int64(0),
		MessageBody:  aws.String(sampleMessage),
		QueueUrl:     &cfg.SqsURL,
	})

	require.NoError(t, err)
	os.Setenv("ENC_SQS_AWS_KEY", cfg.EncSqsAwsKey)
	os.Setenv("ENC_SQS_AWS_SECRET", cfg.EncSqsAwsSecret)
	defer os.Unsetenv("ENC_SQS_AWS_KEY")
	defer os.Unsetenv("ENC_SQS_AWS_SECRET")
	args, err := receiver.BuildArgs()
	require.NoError(t, err)

	msgCount := 0
	msgHandler := func(msg *receiver.FalconMessage) error {
		msgCount++
		assert.Equal(t, "abcdefghijklmn0123456789", msg.CID)
		assert.Equal(t, 1, len(msg.Files))
		assert.Equal(t, cfg.S3Prefix+dataKey, msg.Files[0].Path)
		return nil
	}

	err = receiver.ReceiveMessages(cfg.SqsURL, args.AwsKey, args.AwsSecret, msgHandler)
	require.NoError(t, err)
	assert.Equal(t, 1, msgCount)
}

func TestForwarder(t *testing.T) {
	cfg := loadConfig()

	os.Setenv("ENC_SQS_AWS_KEY", cfg.EncSqsAwsKey)
	os.Setenv("ENC_SQS_AWS_SECRET", cfg.EncSqsAwsSecret)
	defer os.Unsetenv("ENC_SQS_AWS_KEY")
	defer os.Unsetenv("ENC_SQS_AWS_SECRET")
	args, err := receiver.BuildArgs()

	ssn := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	uploader := s3manager.NewUploader(ssn)

	uniqID := uuid.NewV4().String()

	srcKey := cfg.S3Prefix + uniqID + "/src/data.txt"
	dstKey := cfg.S3Prefix + uniqID + "/dst/data.txt"
	// fmt.Println(srcKey)

	// Upload the file to S3.
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(srcKey),
		Body:   strings.NewReader("five timeless words"),
	})
	require.NoError(t, err)

	err = receiver.ForwardS3File(args.AwsKey, args.AwsSecret,
		cfg.S3Region, cfg.S3Bucket, srcKey,
		cfg.S3Region, cfg.S3Bucket, dstKey)

	require.NoError(t, err)

	buf := aws.NewWriteAtBuffer([]byte{})
	downloader := s3manager.NewDownloader(ssn)
	n, err := downloader.Download(buf, &s3.GetObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(dstKey),
	})

	assert.Equal(t, int64(19), n)
	assert.Equal(t, "five timeless words", string(buf.Bytes()))
}
