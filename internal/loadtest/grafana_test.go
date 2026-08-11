package loadtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type grafanaDashboard struct {
	Title      string `json:"title"`
	UID        string `json:"uid"`
	Editable   bool   `json:"editable"`
	Panels     []grafanaPanel
	Templating struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	} `json:"templating"`
}

type grafanaPanel struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Datasource struct {
		UID string `json:"uid"`
	} `json:"datasource"`
	Targets []struct {
		Expression string `json:"expr"`
	} `json:"targets"`
}

func TestGrafanaDashboardIsProvisionable(t *testing.T) {
	root := repositoryRoot(t)
	dashboardPath := filepath.Join(root, "loadtest", "grafana", "dashboards", "goodqueue-loadtest.json")
	contents, err := os.ReadFile(dashboardPath) //nolint:gosec // Repository-relative test fixture path.
	if err != nil {
		t.Fatalf("read Grafana dashboard: %v", err)
	}

	var dashboard grafanaDashboard
	if err := json.Unmarshal(contents, &dashboard); err != nil {
		t.Fatalf("parse Grafana dashboard: %v", err)
	}
	if dashboard.UID != "goodqueue-loadtest" || dashboard.Title == "" {
		t.Fatalf("unexpected dashboard identity: uid=%q title=%q", dashboard.UID, dashboard.Title)
	}
	if dashboard.Editable {
		t.Fatal("provisioned dashboard must be changed through Git, not Grafana UI")
	}
	if len(dashboard.Panels) < 10 {
		t.Fatalf("dashboard has %d panels, want at least 10", len(dashboard.Panels))
	}

	panelIDs := make(map[int]struct{}, len(dashboard.Panels))
	for _, panel := range dashboard.Panels {
		if panel.ID <= 0 || panel.Title == "" {
			t.Fatalf("panel must have stable ID and title: %+v", panel)
		}
		if _, exists := panelIDs[panel.ID]; exists {
			t.Fatalf("duplicate panel ID %d", panel.ID)
		}
		panelIDs[panel.ID] = struct{}{}
		if panel.Datasource.UID != "prometheus" {
			t.Fatalf("panel %q uses datasource %q", panel.Title, panel.Datasource.UID)
		}
		if len(panel.Targets) == 0 {
			t.Fatalf("panel %q has no Prometheus targets", panel.Title)
		}
		for _, target := range panel.Targets {
			if strings.TrimSpace(target.Expression) == "" {
				t.Fatalf("panel %q has an empty PromQL expression", panel.Title)
			}
		}
	}

	variables := make(map[string]struct{}, len(dashboard.Templating.List))
	for _, variable := range dashboard.Templating.List {
		variables[variable.Name] = struct{}{}
	}
	for _, required := range []string{"testid", "scenario"} {
		if _, exists := variables[required]; !exists {
			t.Fatalf("dashboard variable %q is missing", required)
		}
	}
}

func TestGrafanaProvisioningUsesInternalPrometheusAndDisablesAnonymousAccess(t *testing.T) {
	root := repositoryRoot(t)
	datasource := mustReadTextFile(t, filepath.Join(
		root, "loadtest", "grafana", "provisioning", "datasources", "prometheus.yaml",
	))
	if !strings.Contains(datasource, "uid: prometheus") ||
		!strings.Contains(datasource, "url: http://prometheus:9090") ||
		!strings.Contains(datasource, "editable: false") {
		t.Fatalf("Prometheus datasource is not reproducibly provisioned:\n%s", datasource)
	}

	compose := mustReadTextFile(t, filepath.Join(root, "loadtest", "compose.loadtest.yaml"))
	for _, required := range []string{
		"grafana/grafana:12.3.6",
		"GF_AUTH_ANONYMOUS_ENABLED: \"false\"",
		"loadtest/grafana/provisioning/datasources:/etc/grafana/provisioning/datasources:ro",
		"loadtest/grafana/provisioning/dashboards:/etc/grafana/provisioning/dashboards:ro",
		"loadtest/grafana/dashboards:/etc/grafana/dashboards:ro",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("Grafana Compose setting %q is missing", required)
		}
	}
}

func mustReadTextFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path) //nolint:gosec // Callers pass repository-relative test fixture paths.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
