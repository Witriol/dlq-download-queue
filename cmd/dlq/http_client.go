package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func getJSON(url string, out interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func postJSON(url string, payload interface{}, out interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	body, _ := json.Marshal(payload)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readHTTPError(resp *http.Response) error {
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		if msg, ok := body["error"]; ok && msg != "" {
			return fmt.Errorf("http %d: %s", resp.StatusCode, msg)
		}
	}
	return fmt.Errorf("http %d", resp.StatusCode)
}
