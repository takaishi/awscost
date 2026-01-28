package main

import (
	"bytes"
	"fmt"

	"github.com/slack-go/slack"
)

func postToSlack(cfg *Config, text string, graphBuf *bytes.Buffer) error {
	api := slack.New(cfg.SlackBotToken)

	if !dryRun() {
		_, err := api.UploadFileV2(
			slack.UploadFileV2Parameters{
				Reader:         bytes.NewReader(graphBuf.Bytes()),
				FileSize:       graphBuf.Len(),
				Filename:       "daily_costs.png",
				Channel:        cfg.SlackChannelId,
				InitialComment: text,
			})
		if err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}

		return nil
	}
	return nil
}
