package family

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hurseyc/guardtower/internal/notify"
)

// AlertCooldown limits alerts to one per device and category in this window.
const AlertCooldown = 30 * time.Minute

func (s *Service) alertCategories(ctx context.Context) map[string]bool {
	switch s.setting(ctx, SettingAlerts, "adult") {
	case "off":
		return nil
	case "all":
		return map[string]bool{Adult: true, Bypass: true, Gambling: true}
	default:
		return map[string]bool{Adult: true}
	}
}

// alert notifies about newly started sessions, respecting the alert level,
// quiet hours and a per-device cooldown. It also publishes family.alert so
// open browsers can show it.
func (s *Service) alert(ctx context.Context, created []Event) {
	cats := s.alertCategories(ctx)
	if len(cats) == 0 {
		return
	}
	quiet, _ := ParseClock(s.setting(ctx, SettingQuietHours, ""))
	mask, bed := s.masking(ctx), s.bedtime(ctx)
	owners := map[string]string{}
	if rows, err := s.st.DB.QueryContext(ctx, `SELECT pd.device, p.name FROM person_devices pd JOIN people p ON p.id = pd.person_id`); err == nil {
		for rows.Next() {
			var d, n string
			if rows.Scan(&d, &n) == nil {
				owners[d] = n
			}
		}
		rows.Close()
	}
	now := s.Now()
	// Sessions from a backlog (e.g. after downtime) are not news.
	fresh := now.Add(-15 * time.Minute).UnixMilli()
	for _, e := range created {
		if !cats[e.Category] || e.FirstTS < fresh {
			continue
		}
		key := e.Device + "|" + e.Category
		s.mu.Lock()
		last, recent := s.cooldown[key]
		if recent && now.Sub(last) < AlertCooldown {
			s.mu.Unlock()
			continue
		}
		s.cooldown[key] = now
		s.mu.Unlock()

		e.Person = owners[e.Device]
		s.finish(ctx, &e, mask, bed)
		m := alertMessage(e)
		s.bus.Publish("family.alert", map[string]any{"event_id": e.ID, "category": e.Category, "person": e.Person, "title": m.Title, "body": m.Body})
		if quiet.Contains(now) || s.notify == nil {
			continue
		}
		if err := s.notify.SendFamily(ctx, m); err != nil {
			s.log.Warn("family: send alert", "err", err)
		}
		_, _ = s.st.DB.ExecContext(ctx, `UPDATE family_events SET alerted = 1 WHERE id = ?`, e.ID)
	}
}

func alertMessage(e Event) notify.Message {
	subject := e.Person
	if subject == "" {
		subject = e.DeviceName
		if subject == "" {
			subject = e.Device
		}
	} else if e.DeviceName != "" {
		subject += " (" + e.DeviceName + ")"
	}
	what := map[string]string{Adult: "an adult site", //nolint:gosec // display text, not a credential
		Bypass: "a VPN, proxy or private-DNS service", Gambling: "a gambling site"}[e.Category]
	outcome := "blocked"
	if !e.Blocked {
		outcome = "NOT blocked"
	}
	when := time.UnixMilli(e.FirstTS).Format("15:04")
	late := ""
	if e.LateNight {
		late = " during bedtime"
	}
	tags := []string{"warning"}
	prio := "default"
	if !e.Blocked {
		prio = "high"
	}
	return notify.Message{
		Title:    fmt.Sprintf("%s tried %s", subject, what),
		Body:     fmt.Sprintf("%s at %s%s: %s (%s).", subject, when, late, e.Domain, outcome),
		Priority: prio,
		Tags:     tags,
		Event:    "family." + e.Category,
		Data:     map[string]any{"event_id": e.ID, "device": e.Device, "category": e.Category, "blocked": e.Blocked, "masked": e.Masked},
	}
}

// ParseDigest parses "sun 18:00" (or "off").
func ParseDigest(s string) (weekday time.Weekday, minute int, on bool, err error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "off" {
		return 0, 0, false, nil
	}
	day, hm, ok := strings.Cut(s, " ")
	days := map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}
	wd, okd := days[day]
	h, m, okt := strings.Cut(hm, ":")
	hh, e1 := strconv.Atoi(h)
	mm, e2 := strconv.Atoi(m)
	if !ok || !okd || !okt || e1 != nil || e2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false, fmt.Errorf("digest must be \"off\" or like \"sun 18:00\"")
	}
	return wd, hh*60 + mm, true, nil
}

// maybeDigest sends the weekly summary once it is due.
func (s *Service) maybeDigest(ctx context.Context) {
	wd, minute, on, err := ParseDigest(s.setting(ctx, SettingDigest, "sun 18:00"))
	if err != nil || !on || s.notify == nil {
		return
	}
	now := s.Now()
	if now.Weekday() != wd || now.Hour()*60+now.Minute() < minute {
		return
	}
	last, _ := strconv.ParseInt(s.setting(ctx, settingLastDigest, "0"), 10, 64)
	if now.Sub(time.UnixMilli(last)) < 6*24*time.Hour {
		return
	}
	m, err := s.Digest(ctx, now)
	if err != nil {
		s.log.Warn("family: build digest", "err", err)
		return
	}
	if err := s.notify.SendFamily(ctx, m); err != nil {
		s.log.Warn("family: send digest", "err", err)
	}
	_ = s.st.SetSetting(ctx, settingLastDigest, strconv.FormatInt(now.UnixMilli(), 10))
}

// Digest summarises the last seven days.
func (s *Service) Digest(ctx context.Context, now time.Time) (notify.Message, error) {
	o, err := s.Overview(ctx, now.Add(-7*24*time.Hour).UnixMilli(), now.UnixMilli())
	if err != nil {
		return notify.Message{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Adult-site attempts this week: %d", o.Totals.Adult)
	if o.Totals.Allowed > 0 {
		fmt.Fprintf(&b, " (%d not blocked!)", o.Totals.Allowed)
	}
	b.WriteString(".\n")
	for _, p := range o.People {
		if p.Adult == 0 && p.LateNight == 0 {
			fmt.Fprintf(&b, "• %s: no attempts\n", p.Name)
			continue
		}
		fmt.Fprintf(&b, "• %s: %d attempt%s", p.Name, p.Adult, plural(p.Adult))
		if p.LateNight > 0 {
			fmt.Fprintf(&b, ", %d during bedtime", p.LateNight)
		}
		b.WriteString("\n")
	}
	if o.Unassigned.Adult > 0 {
		fmt.Fprintf(&b, "• Unassigned devices: %d attempt%s\n", o.Unassigned.Adult, plural(o.Unassigned.Adult))
	}
	warn := 0
	for _, sg := range o.Signals {
		if sg.Severity == "warn" {
			warn++
		}
	}
	if warn > 0 {
		fmt.Fprintf(&b, "%d possible bypass signal%s; see Family DNS in GuardTower.", warn, plural(int64(warn)))
	}
	return notify.Message{Title: "GuardTower weekly family summary", Body: strings.TrimSpace(b.String()), Event: "family.digest", Tags: []string{"family"}}, nil
}
