package main

import (
	"net/url"
	"testing"
)

func TestGitHubRelayTarget(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		wantOK bool
	}{
		{"/v2/library/nginx/manifests/latest", "", false},
		{"/v2/", "", false},
		{"/", "", false},
		{"/https://api.github.com/repos/a/b/releases/latest", "https://api.github.com/repos/a/b/releases/latest", true},
		{"/https://api.github.com/repos/a/b/releases?per_page=1", "https://api.github.com/repos/a/b/releases?per_page=1", true},
		{"/https://github.com/a/b/releases/download/v1/x.zip", "https://github.com/a/b/releases/download/v1/x.zip", true},
		{"/http://github.com/a/b/archive/main.zip", "http://github.com/a/b/archive/main.zip", true},
		{"/https:/api.github.com/repos/a/b", "https://api.github.com/repos/a/b", true},
	}
	for _, c := range cases {
		u, err := url.Parse("http://proxy.local" + c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		got, ok := githubRelayTarget(u)
		if ok != c.wantOK || got != c.want {
			t.Errorf("githubRelayTarget(%q) = (%q, %v), want (%q, %v)", c.raw, got, ok, c.want, c.wantOK)
		}
	}
}

func TestGitHubHostAllowed(t *testing.T) {
	allowed := []string{
		"github.com", "api.github.com", "codeload.github.com",
		"raw.githubusercontent.com", "objects.githubusercontent.com",
		"media.githubusercontent.com", "API.GitHub.com",
	}
	for _, h := range allowed {
		if !githubHostAllowed(h) {
			t.Errorf("githubHostAllowed(%q) = false, want true", h)
		}
	}
	denied := []string{
		"example.com", "github.com.evil.com", "evilgithub.com",
		"notgithubusercontent.com", "githubusercontent.com.evil.com", "",
	}
	for _, h := range denied {
		if githubHostAllowed(h) {
			t.Errorf("githubHostAllowed(%q) = true, want false", h)
		}
	}
}
