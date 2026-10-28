package api_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/querylog"
)

func TestFamilyAccessAndReveal(t *testing.T) {
	a, c := newTestServer(t)
	ctx := context.Background()
	var res struct {
		CSRF string `json:"csrf_token"`
	}
	c.do("POST", "/api/v1/setup", map[string]string{"username": "admin", "password": "correct-horse-battery"}, &res)
	pw := "x"
	in, _ := a.Fleet.CreateInstance(ctx, fleet.InstanceInput{Name: "agh", URL: "http://127.0.0.1:1", Password: &pw})
	r := querylog.Row{InstanceID: in.ID, TS: time.Now().Add(-time.Minute).UnixMilli(), Client: "10.0.0.7", Domain: "www.pornhub.com", QType: "A", Reason: "FilteredParental"}
	r.Status, r.Blocked = querylog.Classify(r.Reason)
	if _, err := a.QueryLog.Insert(ctx, []querylog.Row{r}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Family.Build(ctx); err != nil {
		t.Fatal(err)
	}

	// A viewer without family access is refused; with it, allowed.
	for _, u := range []struct {
		name   string
		family bool
		code   int
	}{{"viewer", false, 403}, {"parent", true, 200}} {
		if _, err := a.Auth.CreateUser(ctx, u.name, "long-password-1", "viewer", u.family); err != nil {
			t.Fatal(err)
		}
		c.do("POST", "/api/v1/auth/logout", nil, nil)
		c.do("POST", "/api/v1/auth/login", map[string]string{"username": u.name, "password": "long-password-1"}, &res)
		c.csrf = res.CSRF
		if code := c.do("GET", "/api/v1/family/overview", nil, nil); code != u.code {
			t.Fatalf("%s: overview got %d want %d", u.name, code, u.code)
		}
	}

	// The parent sees masked domains and can reveal one; the reveal is audited.
	var evs []struct {
		ID     int64  `json:"id"`
		Domain string `json:"domain"`
		Masked bool   `json:"masked"`
	}
	c.do("GET", "/api/v1/family/events?days=1&category=adult", nil, &evs)
	if len(evs) != 1 || evs[0].Domain != "p***hub.com" || !evs[0].Masked {
		t.Fatalf("events = %+v", evs)
	}
	var rev map[string]string
	if code := c.do("POST", "/api/v1/family/events/"+strconv.FormatInt(evs[0].ID, 10)+"/reveal", nil, &rev); code != 200 || rev["domain"] != "pornhub.com" {
		t.Fatalf("reveal: %d %v", code, rev)
	}
	entries, _ := a.Store.ListAudit(ctx, 10, 0)
	found := false
	for _, e := range entries {
		if e.Action == "family.reveal" {
			found = true
			if strings.Contains(e.Detail, "pornhub.com") && !strings.Contains(e.Detail, "p***hub.com") {
				t.Errorf("audit leaks the domain: %s", e.Detail)
			}
		}
	}
	if !found {
		t.Fatal("reveal not audited")
	}
	// Family settings are validated.
	if code := c.do("PUT", "/api/v1/family/settings", map[string]string{"family.bedtime": "late"}, nil); code != 400 {
		t.Fatalf("bad bedtime accepted: %d", code)
	}
	if code := c.do("PUT", "/api/v1/family/settings", map[string]string{"family.bedtime": "21:30-06:30"}, nil); code != 200 {
		t.Fatalf("bedtime: %d", code)
	}
	// Only admins may apply the Family profile.
	if code := c.do("POST", "/api/v1/family/profile/1", map[string]bool{"parental": true}, nil); code != 403 {
		t.Fatalf("profile by non-admin: %d", code)
	}
}
