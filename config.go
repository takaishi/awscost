package main

import (
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"os"
)

type Config struct {
	SlackBotToken        string `json:"SLACK_BOT_TOKEN"`
	SlackChannelId       string `json:"SLACK_CHANNEL"`
	GetCostAndUsageInput *GetCostAndUsageInput
	Colors               []string
}

type GetCostAndUsageInput struct {
	Filter *types.Expression `json:"Filter"`
}

func NewConfigFromFile(awsConfig aws.Config, path string) (*Config, error) {
	var cfg Config
	fileInfo, err := os.Stat(path)
	if fileInfo != nil && err == nil {
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(buf, &cfg)
		if err != nil {
			return nil, err
		}
	}

	_, exists := os.LookupEnv("SECRET_NAME")
	if exists {
		svc := secretsmanager.NewFromConfig(awsConfig)
		param := &secretsmanager.GetSecretValueInput{
			SecretId:     aws.String(os.Getenv("SECRET_NAME")),
			VersionStage: aws.String("AWSCURRENT"),
		}
		result, err := svc.GetSecretValue(context.TODO(), param)
		if err != nil {
			return nil, err
		}

		var secretString = *result.SecretString
		err = json.Unmarshal([]byte(secretString), &cfg)
		if err != nil {
			return nil, err
		}
	} else {
		cfg.SlackBotToken = os.Getenv("SLACK_BOT_TOKEN")
		cfg.SlackChannelId = os.Getenv("SLACK_CHANNEL")
	}

	return &cfg, nil
}
