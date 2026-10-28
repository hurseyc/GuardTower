package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/auth"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

// fakeIdP is a minimal OpenID provider: /authorize redirects straight back
// with a code, /token returns an ID token for the current user.
type fakeIdP struct {
	*httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	nonce  string
	sub    string
	name   string
	groups []string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeIdP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": p.URL, "authorization_endpoint": p.URL + "/authorize", "token_endpoint": p.URL + "/token",
			"jwks_uri": p.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" {
			http.Error(w, "PKCE required", 400)
			return
		}
		p.mu.Lock()
		p.nonce = q.Get("nonce")
		p.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=abc&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "abc" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad code", 400)
			return
		}
		p.mu.Lock()
		claims := map[string]any{"iss": p.URL, "aud": "guardtower", "sub": p.sub, "nonce": p.nonce,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "preferred_username": p.name, "groups": p.groups}
		p.mu.Unlock()
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		payload, _ := json.Marshal(claims)
		jws, _ := signer.Sign(payload)
		idt, _ := jws.CompactSerialize()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idt})
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

func (p *fakeIdP) as(sub, name string, groups ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sub, p.name, p.groups = sub, name, groups
}

func TestOIDCSignIn(t *testing.T) {
	idp := newFakeIdP(t)
	st, err := store.OpenPath(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	a := app.NewWith(app.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), OIDC: auth.OIDCConfig{
		Issuer: idp.URL, ClientID: "guardtower", ClientSecret: "s3cret", AdminGroups: []string{"gt-admins"},
		ViewerGroups: []string{"family"}, FamilyGroups: []string{"parents"}, Label: "Sign in with Authentik",
	}}, st, box)
	ts := httptest.NewServer(a.Handler())
	defer ts.Close()
	if _, err := a.Auth.CreateUser(context.Background(), "chris", "local-password-1", "admin", true); err != nil {
		t.Fatal(err)
	}

	var meta struct {
		SSO struct{ Label, URL string } `json:"sso"`
	}
	resp, err := http.Get(ts.URL + "/api/v1/meta")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta.SSO.Label != "Sign in with Authentik" {
		t.Fatalf("meta sso: %+v", meta.SSO)
	}

	login := func() (*http.Client, string) {
		jar, _ := cookiejar.New(nil)
		cl := &http.Client{Jar: jar}
		resp, err := cl.Get(ts.URL + meta.SSO.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return cl, resp.Request.URL.String()
	}
	me := func(cl *http.Client) (int, auth.User) {
		resp, err := cl.Get(ts.URL + "/api/v1/me")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct{ User auth.User }
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.User
	}

	// An admin-group member signs in; the local "chris" account is not taken over.
	idp.as("u-1", "chris", "gt-admins")
	cl, landed := login()
	code, u := me(cl)
	if code != 200 || u.Role != "admin" || !u.FamilyAccess || !u.OIDC || u.Username == "chris" || !strings.HasPrefix(u.Username, "chris-") {
		t.Fatalf("admin sign-in: %d %+v (landed on %s)", code, u, landed)
	}
	// Groups are re-read on every sign-in.
	idp.as("u-1", "chris", "family", "parents")
	cl, _ = login()
	if code, u2 := me(cl); code != 200 || u2.ID != u.ID || u2.Role != "viewer" || !u2.FamilyAccess {
		t.Fatalf("re-sign-in: %d %+v", code, u2)
	}
	// Someone in none of the groups is refused with a readable message.
	idp.as("u-2", "stranger", "other")
	cl, landed = login()
	if code, _ := me(cl); code != 401 || !strings.Contains(landed, "sso_error=") {
		t.Fatalf("outsider: %d %s", code, landed)
	}
	// SSO accounts have no password to sign in with.
	if _, err := a.Auth.Authenticate(context.Background(), u.Username, "", "127.0.0.1"); err == nil {
		t.Fatal("password sign-in for an SSO account succeeded")
	}
	// A callback that doesn't match the browser's flow is rejected.
	jar, _ := cookiejar.New(nil)
	resp, err = (&http.Client{Jar: jar}).Get(ts.URL + "/api/v1/auth/oidc/callback?code=abc&state=forged")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(resp.Request.URL.RawQuery, "sso_error=") {
		t.Fatalf("forged callback landed on %s", resp.Request.URL)
	}
}
