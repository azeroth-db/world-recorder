package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"
)

// originLimit is how long Cloudflare waits for an origin before it answers 524 (100 seconds, its documented
// limit); the site cannot answer later than that, so a longer silence is a dead connection.
const originLimit = 100 * time.Second

var client = &http.Client{Transport: transport()}

// transport keeps Go's dial and TLS handshake limits and bounds the wait for an answer.
func transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = originLimit
	return t
}

// do sends the request and ends it once no byte has arrived for originLimit, before the answer or inside its body.
func do(request *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(request.Context())
	idle := time.AfterFunc(originLimit, cancel)
	response, err := client.Do(request.WithContext(ctx))
	if err != nil {
		idle.Stop()
		cancel()
		return nil, err
	}
	response.Body = &idleBody{ReadCloser: response.Body, idle: idle, cancel: cancel}
	return response, nil
}

type idleBody struct {
	io.ReadCloser
	idle   *time.Timer
	cancel context.CancelFunc
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.idle.Reset(originLimit)
	return n, err
}

func (b *idleBody) Close() error {
	b.idle.Stop()
	b.cancel()
	return b.ReadCloser.Close()
}

func get(address string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	return do(request)
}

func postBody(ctx context.Context, address, contentType string, body []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", contentType)
	return do(request)
}
