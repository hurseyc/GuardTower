package api

import (
	"os"
	"strings"
	"testing"
)

// TestSettingsAreDocumented keeps docs/admin/settings.md in step with the
// settings the API accepts.
func TestSettingsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/admin/settings.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range SettingKeys() {
		if !strings.Contains(string(doc), "`"+k+"`") {
			t.Errorf("setting %q is not documented in docs/admin/settings.md", k)
		}
	}
}

// TestConfigReferenceCoversEnv keeps docs/reference/configuration.md in step
// with the environment variables the server reads.
func TestConfigReferenceCoversEnv(t *testing.T) {
	src, err := os.ReadFile("../../cmd/guardtower/main.go")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../../docs/reference/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		for _, part := range strings.Split(line, `"`) {
			if strings.HasPrefix(part, "GT_") && !strings.ContainsAny(part, " %") {
				if !strings.Contains(string(doc), part) {
					t.Errorf("environment variable %s is not documented in docs/reference/configuration.md", part)
				}
			}
		}
	}
}
