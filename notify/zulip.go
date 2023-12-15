package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func SendAlarm(webhook, message string) error {
	values := map[string]string{"text": message}
	json_data, err := json.Marshal(values)

	if err != nil {
		return err
	}

	resp, err := http.Post(webhook, "application/json",
		bytes.NewBuffer(json_data))

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	var res map[string]interface{}

	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return err
	}

	return nil
}
