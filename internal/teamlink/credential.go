// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package teamlink

import (
	"encoding/json"
	"errors"
)

// Credential is what the vault keeps for a joined team server: the device
// token and the server it was issued by. The token is only ever sent to that
// URL — never to the one in teamserver.json, a plain file any program of this
// OS account (the AI included) could rewrite to point at itself.
type Credential struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Encode is the vault value.
func (c Credential) Encode() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// DecodeCredential reads a vault value. A bare token (from before the URL was
// kept with it) or a damaged value counts as no credential: join again.
func DecodeCredential(v string) (Credential, error) {
	var c Credential
	if v == "" {
		return c, errors.New("no device token")
	}
	if json.Unmarshal([]byte(v), &c) != nil || c.Token == "" {
		return Credential{}, errors.New("the device token has no server address with it; leave and join again")
	}
	u, err := CheckURL(c.URL)
	if err != nil || u != c.URL {
		return Credential{}, errors.New("the device token's server address is not valid; leave and join again")
	}
	return c, nil
}

// ErrMoved: teamserver.json names another server than the token belongs to.
var ErrMoved = errors.New("teamserver.json names a different server than the one this device joined; the token is not sent (leave and join again)")

// Bound checks the link against the credential and returns where to send the token.
func (l *Link) Bound(c Credential) error {
	if l == nil || c.URL == "" || l.URL != c.URL {
		return ErrMoved
	}
	return nil
}
