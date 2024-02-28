package main

import (
	"bytes"
	"github.com/slack-go/slack"
)

func postToSlack(cfg *Config, text string, graphBuf *bytes.Buffer) error {
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
