package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
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

// The site moves from wowdb.wiki to azeroth.wiki on November 1, 2026, and wowdb.wiki keeps serving it. A request to
// azeroth.wiki that gets no answer or a server error goes to wowdb.wiki instead; the site stores a resent save once.
const primaryHost, fallbackHost = "azeroth.wiki", "wowdb.wiki"

var primaryDown atomic.Bool

func do(request *http.Request) (*http.Response, error) {
	response, err := send(request)
	if request.URL.Host != primaryHost || request.Context().Err() != nil {
		return response, err
	}
	failed := err != nil || response.StatusCode >= http.StatusInternalServerError
	primaryDown.Store(failed)
	if !failed {
		return response, nil
	}
	if response != nil {
		response.Body.Close()
	}
	retry := request.Clone(request.Context())
	retry.URL.Host, retry.Host = fallbackHost, fallbackHost
	if request.GetBody != nil {
		if retry.Body, err = request.GetBody(); err != nil {
			return nil, err
		}
	}
	return send(retry)
}

// site is the address a browser opens: on wowdb.wiki while azeroth.wiki does not answer.
func site(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Host != primaryHost {
		return address
	}
	if response, err := get(u.Scheme + "://" + primaryHost + "/api/version"); err == nil {
		response.Body.Close()
	}
	if primaryDown.Load() {
		u.Host = fallbackHost
	}
	return u.String()
}

// send sends the request and ends it once no byte has arrived for originLimit, before the answer or inside its body.
func send(request *http.Request) (*http.Response, error) {
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
