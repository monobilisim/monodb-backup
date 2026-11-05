package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"monodb-backup/config"
)

func TestSendAlarmLabelsMongoDB(t *testing.T) {
	oldDatabase, oldWebhook := config.Parameters.Database, webhookStruct
	t.Cleanup(func() {
		config.Parameters.Database, webhookStruct = oldDatabase, oldWebhook
	})

	var body struct {
		Text string `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	config.Parameters.Database = "mongodb"
	webhookStruct = &config.Webhook{Enabled: true, ServerIdentifier: "smoke", Info: []string{server.URL}}
	SendAlarm("backup complete", false)

	want := "[ MongoDB - smoke ] [:check:] backup complete"
	if body.Text != want {
		t.Fatalf("webhook text = %q, want %q", body.Text, want)
	}
}
