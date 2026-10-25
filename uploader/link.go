package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Linking is RFC 8252 loopback with RFC 7636 PKCE: the site hands a one-time grant to this
// computer's loopback address, and only the verifier kept here redeems it for the key.

type linkStart struct {
	URL       string          `json:"url"`
	ExpiresAt json.RawMessage `json:"expiresAt"`
}

var (
	errLinkExpired = errors.New("the link was not approved in time")
	errLinkStopped = errors.New("World Recorder was closed before the link was approved")
)

// link ends when stop is done, withdrawing the request; the caller decides which signals end it.
func (s *state) link(stop context.Context, endpoint string) error {
	base, err := siteBase(endpoint)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes())
	challenge := sha256.Sum256([]byte(verifier))
	var start linkStart
	err = postJSON(stop, base+"/api/contrib/link", map[string]any{
		"label":     computerName(),
		"challenge": base64.RawURLEncoding.EncodeToString(challenge[:]),
		"port":      listener.Addr().(*net.TCPAddr).Port,
	}, &start)
	if err != nil {
		s.log("link result=failed: %v", err)
		return err
	}
	fmt.Println("Approve World Recorder in your browser:", start.URL)
	if err := openBrowser(start.URL); err != nil {
		fmt.Println("Open that address in a browser.")
	}
	s.log("link result=started")
	deadline := expiry(start.ExpiresAt)
	if deadline.IsZero() {
		return errors.New("the link answer has no expiry")
	}
	// Quitting, the deadline or a failure withdraws the request, so the page never sends the player to a closed listener.
	err = s.awaitGrant(stop, listener, base, verifier, deadline)
	if err != nil {
		if address, parsed := url.Parse(start.URL); parsed == nil {
			var answer struct{}
			postJSON(context.Background(), base+"/api/contrib/link/cancel", map[string]string{"request": address.Query().Get("recorder"), "verifier": verifier}, &answer)
		}
	}
	return err
}

// awaitGrant serves the one redirect the site sends after approval, trades its grant for the
// key, and sends the browser back to the site, which opens the World Recorder page.
func (s *state) awaitGrant(stop context.Context, listener net.Listener, base, verifier string, deadline time.Time) error {
	done := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grant := r.URL.Query().Get("grant")
		if r.URL.Path != "/linked" || grant == "" {
			http.NotFound(w, r)
			return
		}
		var answer struct {
			Key string `json:"key"`
			ID  string `json:"id"`
		}
		err := postJSON(context.Background(), base+"/api/contrib/link/token", map[string]string{"grant": grant, "verifier": verifier}, &answer)
		if err == nil && answer.Key == "" {
			err = errors.New("the site answered without a key")
		}
		if err == nil {
			err = pathless(s.storeKey(answer.Key))
		}
		target := base + "/account?linked=" + url.QueryEscape(answer.ID)
		if err != nil {
			target = base + "/account?linked=failed"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		select {
		case done <- err:
		default:
		}
	})}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	select {
	case err := <-done:
		if err != nil {
			s.log("link result=failed: %v", err)
			return err
		}
		s.log("link result=linked")
		fmt.Println("This computer is linked to your wowdb.wiki account.")
		return nil
	case <-time.After(time.Until(deadline)):
		s.log("link result=expired")
		return errLinkExpired
	case <-stop.Done():
		s.log("link result=stopped")
		return errLinkStopped
	}
}

// keyAccepted asks the site whether the stored key still links this computer; a key the site refuses is dropped.
// Offline, the key stands: nothing shows it was unlinked.
func (s *state) keyAccepted(endpoint string) bool {
	key := s.readKey()
	if key == "" {
		return false
	}
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return true
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := do(request)
	if err != nil {
		return true
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		s.log("key result=rejected: this computer must be linked to a wowdb.wiki account again")
		s.dropKey(key)
		return false
	}
	return true
}

// interrupted is done when the player quits a one-shot command or the system ends it.
func interrupted() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

func randomBytes() []byte {
	b := make([]byte, 32)
	rand.Read(b)
	return b
}

func postJSON(ctx context.Context, address string, body, answer any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	response, err := postBody(ctx, address, "application/json", data)
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
