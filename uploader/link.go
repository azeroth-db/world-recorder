package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RFC 8628 section 3.5: a client polls every 5 seconds when the server names no interval.
const linkPollInterval = 5 * time.Second

type linkStart struct {
	Code      string          `json:"code"`
	Poll      string          `json:"poll"`
	URL       string          `json:"url"`
	ExpiresAt json.RawMessage `json:"expiresAt"`
}

type linkAnswer struct {
	Status string `json:"status"`
	Key    string `json:"key"`
}

var errLinkExpired = errors.New("the link code expired before it was confirmed")

func (s *state) link(endpoint string) error {
	base, err := siteBase(endpoint)
	if err != nil {
		return err
	}
	var start linkStart
	if err := postJSON(base+"/api/contrib/link", map[string]string{"label": computerName()}, &start); err != nil {
		s.log("link result=failed: %v", err)
		return err
	}
	if start.Poll == "" || start.URL == "" {
		return errors.New("the link answer has no poll secret or address")
	}
	fmt.Printf("Confirm code %s in your browser at %s\n", start.Code, start.URL)
	if err := openBrowser(start.URL); err != nil {
		fmt.Println("Open that address in a browser.")
	}
	s.log("link result=started")
	return s.awaitLink(base, start.Poll, expiry(start.ExpiresAt))
}

func (s *state) awaitLink(base, poll string, deadline time.Time) error {
	for deadline.IsZero() || time.Now().Before(deadline) {
		time.Sleep(linkPollInterval)
		var answer linkAnswer
		err := postJSON(base+"/api/contrib/link/poll", map[string]string{"poll": poll}, &answer)
		var status statusError
		switch {
		case errors.As(err, &status) && status == http.StatusGone:
			s.log("link result=expired")
			return errLinkExpired
		case errors.As(err, &status) && status != http.StatusTooManyRequests && status < 500:
			s.log("link result=failed: %v", err)
			return err
		case err != nil:
			s.log("link poll result=failed: %v", err)
		case answer.Status == "linked" && answer.Key != "":
			if err := s.storeKey(answer.Key); err != nil {
				return pathless(err)
			}
			s.log("link result=linked")
			fmt.Println("This computer is linked to your wowdb.wiki account.")
			return nil
		case answer.Status == "expired":
			s.log("link result=expired")
			return errLinkExpired
		}
	}
	s.log("link result=expired")
	return errLinkExpired
}

func postJSON(address string, body, answer any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	response, err := http.Post(address, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return statusError(response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(answer)
}

func siteBase(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("endpoint %q is not an absolute URL", endpoint)
	}
	return u.Scheme + "://" + u.Host, nil
}

func expiry(raw json.RawMessage) time.Time {
	var millis int64
	if json.Unmarshal(raw, &millis) == nil {
		return time.UnixMilli(millis)
	}
	var text string
	json.Unmarshal(raw, &text)
	at, _ := time.Parse(time.RFC3339, text)
	return at
}

func computerName() string {
	name, _ := os.Hostname()
	return strings.TrimSuffix(name, ".local")
}
