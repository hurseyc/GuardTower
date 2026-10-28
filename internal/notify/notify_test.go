package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/store"
)

func TestDeliveryFormats(t *testing.T) {
	ctx := context.Background()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(make([]byte, 32))
	s := New(st, box)
	got := map[string]*http.Request{}
	bodies := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got[r.URL.Path], bodies[r.URL.Path] = r, string(b)
	}))
	defer srv.Close()
	tok := "s3cret"
	for _, in := range []TargetInput{
		{Name: "phones", Kind: KindNtfy, URL: srv.URL + "/ntfy", Token: &tok, Enabled: true, Family: true},
		{Name: "discord", Kind: KindDiscord, URL: srv.URL + "/discord", Enabled: true, Family: true},
		{Name: "hook", Kind: KindWebhook, URL: srv.URL + "/hook", Enabled: true, Family: true},
		{Name: "off", Kind: KindWebhook, URL: srv.URL + "/off", Enabled: false, Family: true},
	} {
		if _, err := s.Save(ctx, 0, in); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SendFamily(ctx, Message{Title: "Sam tried an adult site", Body: "at 23:14: p***hub.com (blocked)", Priority: "high", Tags: []string{"warning"}, Event: "family.adult"}); err != nil {
		t.Fatal(err)
	}
	n := got["/ntfy"]
	if n == nil || n.Header.Get("Title") != "Sam tried an adult site" || n.Header.Get("Priority") != "high" || n.Header.Get("Authorization") != "Bearer s3cret" || !strings.Contains(bodies["/ntfy"], "p***hub.com") {
		t.Fatalf("ntfy request: %+v %q", n, bodies["/ntfy"])
	}
	var d map[string]any
	_ = json.Unmarshal([]byte(bodies["/discord"]), &d)
	if !strings.HasPrefix(d["content"].(string), "**Sam tried an adult site**") {
		t.Fatalf("discord body: %s", bodies["/discord"])
	}
	var h Message
	_ = json.Unmarshal([]byte(bodies["/hook"]), &h)
	if h.Event != "family.adult" {
		t.Fatalf("webhook body: %s", bodies["/hook"])
	}
	if got["/off"] != nil {
		t.Fatal("disabled target received a message")
	}
	ts, _ := s.List(ctx)
	for _, x := range ts {
		if x.Name == "phones" && !x.HasToken {
			t.Fatal("token not stored")
		}
	}
	if _, err := s.Save(ctx, 0, TargetInput{Name: "x", Kind: KindNtfy, URL: "ftp://nope"}); err == nil {
		t.Fatal("invalid URL accepted")
	}
}
