package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSortLabels(t *testing.T) {
	mrs := []MR{{}}
	mrs[0].Labels.Nodes = []Label{{Title: "platform::android"}, {Title: "priority::next"}, {Title: "Autoreview::clean"}}
	sortLabels(mrs)
	want := []string{"Autoreview::clean", "platform::android", "priority::next"}
	for i, l := range mrs[0].Labels.Nodes {
		if l.Title != want[i] {
			t.Fatalf("got %v, want %v", mrs[0].Labels.Nodes, want)
		}
	}
}

func TestHideLabels(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	if err := os.WriteFile(path, []byte("hide_labels:\n  - \"^platform::\"\n  - \"^priority::(next|later)$\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.visibleLabels([]Label{
		{Title: "autoreview::clean"}, {Title: "platform::android"}, {Title: "priority::next"}, {Title: "priority::now"},
	})
	if len(got) != 2 || got[0].Title != "autoreview::clean" || got[1].Title != "priority::now" {
		t.Errorf("got %v", got)
	}

	if err := os.WriteFile(path, []byte("hide_labels:\n  - \"(\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadConfig(path); err == nil {
		t.Error("an invalid pattern should fail to load")
	}
}

func TestShowLabels(t *testing.T) {
	path := t.TempDir() + "/config.yaml"
	cfgText := "show_labels:\n  - \"^autoreview::\"\n  - \"^bug$\"\nhide_labels:\n  - \"::running$\"\n"
	if err := os.WriteFile(path, []byte(cfgText), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.visibleLabels([]Label{
		{Title: "autoreview::clean"}, {Title: "autoreview::running"}, {Title: "bug"}, {Title: "bugfix"}, {Title: "platform::android"},
	})
	if len(got) != 2 || got[0].Title != "autoreview::clean" || got[1].Title != "bug" {
		t.Errorf("got %v", got)
	}
}

func TestExpiredGlabTokenIsRenewed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":{"currentUser":{"username":"jane","authoredMergeRequests":{"pageInfo":{"hasNextPage":false},"nodes":[{"iid":"1"}]}}}}`))
	}))
	defer srv.Close()

	renewed := 0
	c := &Client{baseURL: srv.URL, http: srv.Client(), token: "expired",
		reload: func() (string, error) { return "expired", nil },
		renew:  func(context.Context) (string, error) { renewed++; return "fresh", nil },
	}
	mrs, me, err := c.FetchMRs(context.Background(), nil)
	if err != nil || me != "jane" || len(mrs) != 1 || renewed != 1 {
		t.Fatalf("got mrs=%v me=%q err=%v renewed=%d", mrs, me, err, renewed)
	}

	// A token that isn't from glab is never renewed; the 401 is reported as is.
	c = &Client{baseURL: srv.URL, http: srv.Client(), token: "wrong"}
	if _, _, err := c.FetchMRs(context.Background(), nil); !errors.Is(err, errUnauthorized) || strings.Contains(err.Error(), "glab") {
		t.Fatalf("want a plain 401, got %v", err)
	}
}
