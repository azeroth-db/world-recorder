package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
)

// apiAddress is fixed so the profile page can find the program; it is a variable only so a test build can serve
// another port (-ldflags -X main.apiAddress=...).
var apiAddress = "127.0.0.1:47930"

func (d *daemon) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, d.status()) })
	mux.HandleFunc("GET /v1/events", d.events)
	mux.HandleFunc("GET /v1/saves/{id}", d.saveData)
	mux.HandleFunc("POST /v1/send", d.command(func(any) (int, error) { d.sendNow(); return http.StatusAccepted, nil }, nil))
	mux.HandleFunc("POST /v1/settings", d.command(func(body any) (int, error) {
		patch := *body.(*settingsPatch)
		return http.StatusOK, d.await("", func() error { return d.applySettings(patch) })
	}, func() any { return new(settingsPatch) }))
	mux.HandleFunc("POST /v1/folder", d.command(func(any) (int, error) { return http.StatusOK, d.await("folder", d.chooseFolder) }, nil))
	mux.HandleFunc("POST /v1/addon", d.command(func(any) (int, error) { return http.StatusOK, d.await("addon", d.installAddon) }, nil))
	mux.HandleFunc("POST /v1/update", d.command(d.start("update", d.updateNow), nil))
	mux.HandleFunc("POST /v1/link", d.command(func(any) (int, error) { return http.StatusAccepted, d.startLink() }, nil))
	mux.HandleFunc("POST /v1/uninstall", d.leave)
	return d.guard(mux)
}

// guard admits only requests to the loopback address from the site's origin or the desktop app, whose commands carry
// the control token; status and events also answer a terminal.
func (d *daemon) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Origin")
		if r.Host != apiAddress {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		origin := r.Header.Get("Origin")
		terminal := origin == "" && r.Method == http.MethodGet && (r.URL.Path == "/v1/status" || r.URL.Path == "/v1/events")
		desktop := origin == "" && d.controlled(r.Header.Get("X-Recorder-Control"))
		if origin != d.origin && !terminal && !desktop {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		if r.Method == http.MethodOptions {
			preflight(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func preflight(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Access-Control-Allow-Methods", "GET, POST")
	h.Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		h.Set("Access-Control-Allow-Private-Network", "true")
	}
	w.WriteHeader(http.StatusNoContent)
}

// command reads the JSON body into newBody's value (an empty object when nil), runs the command and answers with the status.
func (d *daemon) command(run func(any) (int, error), newBody func() any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind != "application/json" {
			http.Error(w, "commands are JSON", http.StatusUnsupportedMediaType)
			return
		}
		body := any(&struct{}{})
		if newBody != nil {
			body = newBody()
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		code, err := run(body)
		switch {
		case errors.Is(err, errCancelled), errors.Is(err, errBusy):
			code = http.StatusConflict
		case err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": pathless(err).Error()})
			return
		}
		writeJSON(w, code, d.status())
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// await runs the command on the worker and returns its result.
func (d *daemon) await(busy string, run func() error) error {
	done, err := d.submit(busy, run)
	if err != nil {
		return err
	}
	return <-done
}

// start runs the command on the worker and answers at once; its result arrives as a status event.
func (d *daemon) start(busy string, run func() error) func(any) (int, error) {
	return func(any) (int, error) {
		done, err := d.submit(busy, run)
		if err != nil {
			return 0, err
		}
		go func() {
			if err := <-done; err != nil {
				d.s.log("%s result=failed: %v", busy, pathless(err))
			}
		}()
		return http.StatusAccepted, nil
	}
}

// leave answers first: on macOS removing the login item ends this process.
func (d *daemon) leave(w http.ResponseWriter, r *http.Request) {
	accepted := false
	d.command(func(any) (int, error) { accepted = true; return http.StatusAccepted, nil }, nil)(w, r)
	if accepted {
		d.uninstall()
	}
}

func (d *daemon) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	changes := d.subscribe()
	defer d.unsubscribe(changes)
	for {
		data, err := json.Marshal(d.status())
		if err != nil {
			return
		}
		fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
		flusher.Flush()
		select {
		case <-changes:
		case <-r.Context().Done():
			return
		}
	}
}

// saveData answers the save converted to JSON exactly as it is uploaded.
func (d *daemon) saveData(w http.ResponseWriter, r *http.Request) {
	for _, sv := range findSaves(installs(d.gameFolder())) {
		if digest(sv.path) != r.PathValue("id") {
			continue
		}
		c, err := d.s.convert(sv)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(c.data)
		return
	}
	http.NotFound(w, r)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	data, err := encodeJSON(value)
	if err != nil {
		code, data = http.StatusInternalServerError, []byte(`{"error":"encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(code)
	w.Write(data)
}
