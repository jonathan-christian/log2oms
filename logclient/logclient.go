package logclient

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
)

var (
	locationGMT = time.FixedZone("GMT", 0)
)

// LogClient is the client for log analytics
type LogClient struct {
	workspaceID     string
	workspaceSecret string
	logType         string
	httpClient      *http.Client
	signingKey      []byte
	apiLogsURL      string
	metadata        map[string]string
}

// NewLogClient creates a log client
func NewLogClient(workspaceID, workspaceSecret, logType string, metadata map[string]string) LogClient {
	client := LogClient{
		workspaceID:     workspaceID,
		workspaceSecret: workspaceSecret,
		logType:         logType,
		metadata:        metadata,
	}

	if client.metadata == nil {
		client.metadata = map[string]string{}
	}

	client.httpClient = &http.Client{Timeout: time.Second * 30}
	client.signingKey, _ = base64.StdEncoding.DecodeString(workspaceSecret)
	client.apiLogsURL = fmt.Sprintf("https://%s.ods.opinsights.azure.com/api/logs?api-version=2016-04-01", workspaceID)

	return client
}

// PostMessage logs a single message to log analytics service
func (c *LogClient) PostMessage(message string, timestamp time.Time) error {
	return c.PostMessages([]string{message}, timestamp)
}

// PostMessages logs an array of messages to log analytics service
func (c *LogClient) PostMessages(messages []string, timestamp time.Time) error {
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	var logs []map[string]string
	for _, m := range messages {
		log := make(map[string]string, len(c.metadata)+1)
		for item := range c.metadata {
			log[item] = c.metadata[item]
		}
		log["message"] = m
		log["Timestamp"] = timestamp.Format(time.RFC3339)

		logs = append(logs, log)
	}

	body, _ := json.Marshal(logs)
	req, _ := http.NewRequest(http.MethodPost, c.apiLogsURL, bytes.NewReader(body))

	date := time.Now().In(locationGMT).Format(time.RFC1123)
	stringToSign := "POST\n" + strconv.FormatInt(req.ContentLength, 10) + "\napplication/json\n" + "x-ms-date:" + date + "\n/api/logs"

	signature := computeHmac256(stringToSign, c.signingKey)

	req.Header.Set("Authorization", fmt.Sprintf("SharedKey %s:%s", c.workspaceID, signature))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Log-Type", c.logType)
	req.Header.Set("x-ms-date", date)
	req.Header.Set("time-generated-field", "Timestamp")

	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to post request: %v", err)
	}

	if response.StatusCode != 200 {
		defer response.Body.Close()
		buf, _ := ioutil.ReadAll(response.Body)

		time.AfterFunc(
			time.Second*15,
			func() {
				err := c.PostMessages(messages, timestamp)
				if err != nil {
					fmt.Printf("[LOG2OMS][%s] Retry failed, will keep retrying", time.Now().UTC().Format(time.RFC3339))
				} else {
					fmt.Printf("[LOG2OMS][%s] Posted %d messages.\n", time.Now().UTC().Format(time.RFC3339), len(logs))
				}
			})

		return fmt.Errorf("[LOG2OMS][%s] Post log request failed with status: %d %s", time.Now().UTC().Format(time.RFC3339), response.StatusCode, string(buf))
	}

	fmt.Printf("[LOG2OMS][%s] Posted %d messages.\n", time.Now().UTC().Format(time.RFC3339), len(logs))

	return nil
}
