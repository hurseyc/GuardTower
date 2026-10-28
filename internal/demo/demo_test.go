package demo_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hurseyc/guardtower/internal/app"
	"github.com/hurseyc/guardtower/internal/crypto"
	"github.com/hurseyc/guardtower/internal/demo"
	"github.com/hurseyc/guardtower/internal/fleet"
	"github.com/hurseyc/guardtower/internal/rollout"
	"github.com/hurseyc/guardtower/internal/store"
)

// TestDemoFleet seeds the demo and applies the pending Kids VLAN change, so
// the simulator stays faithful enough for reads, applies and health probes.
func TestDemoFleet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st, err := store.OpenPath(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := crypto.New(bytes.Repeat([]byte{7}, 32))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := app.NewWith(app.Config{Log: log, StatusInterval: 200 * time.Millisecond, CollectInterval: time.Second}, st, box)
	f, err := demo.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := demo.Seed(ctx, a, f, log); err != nil {
		t.Fatal(err)
	}
	a.Start(ctx)

	ins, err := a.Fleet.ListInstances(ctx)
	if err != nil || len(ins) != 5 {
		t.Fatalf("instances: %v %d", err, len(ins))
	}
	var kids []*fleet.Instance
	for i := range ins {
		sum, err := a.Config.CheckInstance(ctx, ins[i])
		if err != nil {
			t.Fatal(err)
		}
		if len(sum.Errors) > 0 {
			t.Fatalf("%s: %v", ins[i].Name, sum.Errors)
		}
		want := 0
		if ins[i].Name == "agh-4" || ins[i].Name == "agh-5" {
			want = sum.Total // Safe Search is pending
			if sum.Sections["safesearch"] == 0 {
				t.Fatalf("%s: expected a pending Safe Search change, got %+v", ins[i].Name, sum)
			}
			kids = append(kids, ins[i])
		}
		if sum.Total != want {
			t.Fatalf("%s should be in sync after seeding: %+v", ins[i].Name, sum)
		}
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		in, _ := a.Fleet.GetInstance(ctx, kids[0].ID)
		if in.Status != nil && in.Status.Online {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("instances never came online")
		}
		time.Sleep(100 * time.Millisecond)
	}
	id, err := a.Rollout.Start(ctx, rollout.Target{Kind: "group", ID: kids[0].GroupID}, nil, demo.User)
	if err != nil {
		t.Fatal(err)
	}
	var ro *rollout.Rollout
	for deadline = time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if ro, err = a.Rollout.Get(ctx, id); err != nil {
			t.Fatal(err)
		}
		if ro.FinishedAt != 0 {
			break
		}
	}
	if ro.Status != "succeeded" {
		t.Fatalf("rollout: %s %s %+v", ro.Status, ro.Error, ro.Steps)
	}
	for _, in := range kids {
		sum, err := a.Config.CheckInstance(ctx, in)
		if err != nil || sum.Total != 0 {
			t.Fatalf("%s after apply: %v %+v", in.Name, err, sum)
		}
	}
}
