package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"pgsql-backup/config"
)

type Mattermost struct {
	p *config.Params
}

type Payload struct {
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
	Props     Props  `json:"props"`
}
type Attachments struct {
	Pretext string `json:"pretext"`
	Text    string `json:"text"`
}
type Props struct {
	Attachments []Attachments `json:"attachments"`
}

func NewMattermost(params *config.Params) (m *Mattermost) {
	m = &Mattermost{
		p: params,
	}
	return
}

func (m *Mattermost) Notify(message string, pretext string, text string) {
	if m.p.Fqdn != "" {
		message = "**[" + m.p.Fqdn + "]** " + message
	}

	data := Payload{
		ChannelID: m.p.Mattermost.ChannelId,
		Message:   message,
	}

	if text != "" {
		data.Props.Attachments = append(
			data.Props.Attachments,
			Attachments{
				Pretext: pretext,
				Text:    text,
			},
		)
	}

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshalling message payload for Mattermost: %q", err.Error())
		return
	}
	body := bytes.NewReader(payloadBytes)

	req, err := http.NewRequest("POST", m.p.Mattermost.Url + "/api/v4/posts", body)
	if err != nil {
		log.Printf("Error creating POST request for Mattermost: %q", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.p.Mattermost.ApiToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error sending POST request to Mattermost: %q", err.Error())
		return
	}
	statusOK := res.StatusCode >= 200 && res.StatusCode < 300
	if !statusOK {
		log.Printf("Non-OK HTTP status sending POST request to Mattermost: %q", res.Status)
		return
	}

	defer func() {
		err := res.Body.Close()
		if err != nil {
			log.Printf("Error closing response body from Mattermost: %q", err.Error())
			return
		}
	}()
}
