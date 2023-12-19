package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/slack-go/slack"
	"os"
)

func postToSlack(awsConfig aws.Config, text string, graphBuf *bytes.Buffer) error {
	cfg := &Config{}
	_, exists := os.LookupEnv("SECRET_NAME")
	if exists {
		svc := secretsmanager.NewFromConfig(awsConfig)
		param := &secretsmanager.GetSecretValueInput{
			SecretId:     aws.String(os.Getenv("SECRET_NAME")),
			VersionStage: aws.String("AWSCURRENT"),
		}
		result, err := svc.GetSecretValue(context.TODO(), param)
		if err != nil {
			return err
		}

		var secretString = *result.SecretString
		err = json.Unmarshal([]byte(secretString), cfg)
		if err != nil {
			return err
		}
	} else {
		cfg.SlackBotToken = os.Getenv("SLACK_BOT_TOKEN")
		cfg.SlackChannel = os.Getenv("SLACK_CHANNEL")
	}

	api := slack.New(cfg.SlackBotToken)

	if !dryRun() {
		opts := []slack.MsgOption{
			slack.MsgOptionText(text, false),
		}

		_, _, err := api.PostMessage(
			cfg.SlackChannel,
			opts...,
		)

		_, err = api.UploadFile(
			slack.FileUploadParameters{
				Reader:         graphBuf,
				Filename:       "daily_costs.png",
				InitialComment: "アカウント別の日次料金(90日分)",
				Channels:       []string{cfg.SlackChannel},
			})
		if err != nil {
			return err
		}

		return err
	}
	return nil
}
