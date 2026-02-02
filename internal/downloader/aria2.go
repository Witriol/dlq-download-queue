package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type Aria2Client struct {
	Endpoint string
	Secret   string
	Client   *http.Client
}

func NewAria2Client(endpoint, secret string) *Aria2Client {
	return &Aria2Client{
		Endpoint: endpoint,
		Secret:   secret,
		Client:   &http.Client{Timeout: 10 * time.Second},
	}
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a *Aria2Client) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	p := params
	if a.Secret != "" {
		p = append([]interface{}{"token:" + a.Secret}, params...)
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: "dlq", Method: method, Params: p})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("aria2_rpc_error:%d:%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}

func (a *Aria2Client) AddURI(ctx context.Context, uri string, options map[string]string) (string, error) {
	params := []interface{}{[]string{uri}, options}
	var gid string
	if err := a.call(ctx, "aria2.addUri", params, &gid); err != nil {
		return "", err
	}
	return gid, nil
}

type Status struct {
	GID           string `json:"gid"`
	Status        string `json:"status"`
	TotalLength   string `json:"totalLength"`
	CompletedLen  string `json:"completedLength"`
	DownloadSpeed string `json:"downloadSpeed"`
	ErrorCode     string `json:"errorCode"`
	ErrorMessage  string `json:"errorMessage"`
	Files         []struct {
		Path string `json:"path"`
	} `json:"files"`
}

func (a *Aria2Client) TellStatus(ctx context.Context, gid string) (*Status, error) {
	var st Status
	params := []interface{}{gid, []string{"gid", "status", "totalLength", "completedLength", "downloadSpeed", "errorCode", "errorMessage", "files"}}
	if err := a.call(ctx, "aria2.tellStatus", params, &st); err != nil {
		return nil, err
	}
	if st.GID == "" {
		return nil, errors.New("aria2_empty_status")
	}
	return &st, nil
}

func (a *Aria2Client) TellActive(ctx context.Context) ([]Status, error) {
	var out []Status
	params := []interface{}{[]string{"gid", "status", "totalLength", "completedLength", "errorCode", "errorMessage"}}
	if err := a.call(ctx, "aria2.tellActive", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *Aria2Client) TellWaiting(ctx context.Context, offset, num int) ([]Status, error) {
	var out []Status
	params := []interface{}{offset, num, []string{"gid", "status", "totalLength", "completedLength", "errorCode", "errorMessage"}}
	if err := a.call(ctx, "aria2.tellWaiting", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *Aria2Client) TellStopped(ctx context.Context, offset, num int) ([]Status, error) {
	var out []Status
	params := []interface{}{offset, num, []string{"gid", "status", "totalLength", "completedLength", "errorCode", "errorMessage"}}
	if err := a.call(ctx, "aria2.tellStopped", params, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *Aria2Client) Pause(ctx context.Context, gid string) error {
	return a.call(ctx, "aria2.pause", []interface{}{gid}, nil)
}

func (a *Aria2Client) Unpause(ctx context.Context, gid string) error {
	return a.call(ctx, "aria2.unpause", []interface{}{gid}, nil)
}

func (a *Aria2Client) Remove(ctx context.Context, gid string) error {
	return a.call(ctx, "aria2.remove", []interface{}{gid}, nil)
}
