package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	// ContentTypeJSON is a Content-Type header for json formatted logs
	ContentTypeJSON = "application/json"
	// ContentTypeGzipJSON is a Content-Type header for gzipped json formatted logs
	ContentTypeGzipJSON = "application/gzip"
	// HeaderLicenseKey is for use with new relic license key
	HeaderLicenseKey = "X-License-Key"
	// HeaderInsertKey is for use with new relic insert api key
	HeaderInsertKey = "Api-Key"
	// USLogsEndpoint is the new relic logs endpoint for US users
	USLogsEndpoint = "https://log-api.newrelic.com/log/v1"
	// EULogsEndpoint is the new relig logs endpoint for EU users
	EULogsEndpoint = "https://log-api.eu.newrelic.com/log/v1"
)

type nrLog struct {
	Timestamp   int    `json:"timestamp"`
	Message     string `json:"message"`
	LogType     string `json:"logtype,omitempty"`
	OtherFields string `json:"other_fields,omitempty"`
}

// NRLogProcessor sends logs to new relic log api. Impliments io.Writer
type NRLogProcessor struct {
	now              func() time.Time
	authHeaderKey    string
	authHeaderValue  string
	contentTypeValue string
	url              string
}

// NewEUNRLogProcessorWithLicenseKey returns NRLogProcessor to send logs to new relic logs api
func NewEUNRLogProcessorWithLicenseKey(key string) NRLogProcessor {
	return NRLogProcessor{
		now:              time.Now,
		authHeaderKey:    HeaderLicenseKey,
		authHeaderValue:  key,
		contentTypeValue: ContentTypeJSON,
		url:              EULogsEndpoint,
	}
}

// NewEUNRLogProcessorWithInsertKey returns NRLogProcessor to send logs to new relic logs api
func NewEUNRLogProcessorWithInsertKey(key string) NRLogProcessor {
	return NRLogProcessor{
		now:              time.Now,
		authHeaderKey:    HeaderInsertKey,
		authHeaderValue:  key,
		contentTypeValue: ContentTypeJSON,
		url:              EULogsEndpoint,
	}
}

func (nr NRLogProcessor) Write(b []byte) (n int, err error) {
	none := 0
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	err = enc.Encode(&nrLog{
		Timestamp: int(nr.now().UnixNano() / int64(time.Millisecond)),
		Message:   string(b),
	})
	if err != nil {
		return none, fmt.Errorf("could not encode log to json: %w", err)
	}
	c := http.DefaultClient
	req, err := http.NewRequest(http.MethodPost, nr.url, buf)
	if err != nil {
		return none, fmt.Errorf("could not build request for sending log: %w", err)
	}
	req.Header.Set(nr.authHeaderKey, nr.authHeaderValue)
	req.Header.Set("Content-Type", nr.contentTypeValue)
	_, err = c.Do(req)
	if err != nil {
		return none, fmt.Errorf("could not send log to endpoint: %w", err)
	}
	// if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
	// 	body, err := ioutil.ReadAll(resp.Body)
	// 	defer resp.Body.Close()
	// 	return none, fmt.Errorf("unexpected status code: %s: %s: %w", resp.Status, string(body), err)
	// }
	return len(b), nil
}
