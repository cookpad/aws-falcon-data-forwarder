package main_test

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
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

	sampleMessage := `{
		"cid": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"timestamp": 1492726639137,
		"fileCount": 4,
		"totalSize": 349986220,
		"bucket": "cs-prod-cannon-xxxxxxxxxxxxxxxx",
		"pathPrefix": "data/f0714ca5-3689-448d-b5cc-582a6f7a56b1",
		"files": [
		  {
			"path": "data/f0714ca5-3689-448d-b5cc-582a6f7a56b1/part-00000.gz",
			"size": 90506436,
			"checksum": "69fe068dd7d115ebdc21ed4181b4cd79"
		  },
		  {
			"path": "data/f0714ca5-3689-448d-b5cc-582a6f7a56b1/part-00001.gz",
			"size": 86467595,
			"checksum": "7d0185c02e0d50f8b8584729be64318b"
		  },
		  {
			"path": "data/f0714ca5-3689-448d-b5cc-582a6f7a56b1/part-00002.gz",
			"size": 83893709,
			"checksum": "7c36641d7bb3e1bb4526ddc4c1655017"
		  },
		  {
			"path": "data/f0714ca5-3689-448d-b5cc-582a6f7a56b1/part-00003.gz",
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
		return nil
	}

	err = receiver.ReceiveMessages(cfg.SqsURL, args.AwsKey, args.AwsSecret, msgHandler)
	require.NoError(t, err)
	assert.Equal(t, 1, msgCount)
}
