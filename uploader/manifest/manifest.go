// Package manifest is release.json, the file installed programs read to update themselves, and its signature.
package manifest

import (
	"crypto/ed25519"
	"encoding/base64"
)

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Release is release.json. Its addon archive is what the program installs, and what players who install by hand
// download.
type Release struct {
	Version   string `json:"version"`
	Darwin    Asset  `json:"darwin"`
	Windows   Asset  `json:"windows"`
	Addon     Asset  `json:"addon"`
	Signature string `json:"signature,omitempty"`
}

// Message is what the signature covers: each field on a line of its own, in this order.
func (r Release) Message() []byte {
	var text string
	for _, field := range []string{r.Version, r.Darwin.URL, r.Darwin.SHA256, r.Windows.URL, r.Windows.SHA256, r.Addon.URL, r.Addon.SHA256} {
		text += field + "\n"
	}
	return []byte(text)
}

func (r *Release) Sign(key ed25519.PrivateKey) {
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, r.Message()))
}

func (r Release) Verify(key ed25519.PublicKey) bool {
	signature, err := base64.StdEncoding.DecodeString(r.Signature)
	return err == nil && ed25519.Verify(key, r.Message(), signature)
}
