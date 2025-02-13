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
			cfg.SlackChannelId,
			opts...,
		)

		_, err = api.UploadFileV2(
			slack.UploadFileV2Parameters{
				Reader:         graphBuf,
				FileSize:       graphBuf.Len(),
				Filename:       "daily_costs.png",
				InitialComment: "アカウント別の日次料金(90日分)",
				Channel:        cfg.SlackChannelId,
			})
		if err != nil {
			return err
		}

		return err
	}
	return nil
}
