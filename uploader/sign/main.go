// sign signs release.json for publish.sh; it is never built into the program.
//
//	go run ./sign generate <key file>   writes a new private key and prints its public key
//	go run ./sign <key file>            reads release.json on standard input and writes it signed
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"wowdb.wiki/recorder-uploader/manifest"
)

func main() {
	var err error
	switch args := os.Args[1:]; {
	case len(args) == 2 && args[0] == "generate":
		err = generate(args[1])
	case len(args) == 1:
		err = sign(args[0])
	default:
		err = errors.New("usage: sign generate <key file> | sign <key file> < release.json")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func generate(path string) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(base64.StdEncoding.EncodeToString(private.Seed()) + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		fmt.Println(base64.StdEncoding.EncodeToString(public))
	}
	return err
}

func sign(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("the key file holds no ed25519 key")
	}
	var r manifest.Release
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&r); err != nil {
		return err
	}
	r.Sign(ed25519.NewKeyFromSeed(seed))
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}
