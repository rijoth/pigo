package fantasy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pigo/internal/models"
)

func TestOpencodeAttributionHeaders(t *testing.T) {
	headers := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
	}))
	defer srv.Close()

	do := func(client *http.Client) http.Header {
		req, err := http.NewRequest(http.MethodPost, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return <-headers
	}

	session := "sess-one"
	client := attributionClient("opencode-go", nil, func() string { return session })

	if h := do(client); h.Get("x-opencode-session") != "sess-one" {
		t.Fatalf("opencode-go session header = %q", h.Get("x-opencode-session"))
	} else if h.Get("User-Agent") != "pigo/"+Version {
		t.Fatalf("opencode-go user-agent = %q", h.Get("User-Agent"))
	}

	if h := do(attributionClient("anthropic", nil, func() string { return session })); h.Get("x-opencode-session") != "" ||
		strings.HasPrefix(h.Get("User-Agent"), "pigo/") {
		t.Fatalf("non-opencode got attribution headers: %v", h)
	}

	override := attributionClient("opencode-go", map[string]string{
		"X-OpenCode-Session": "explicit",
		"user-agent":         "custom/1",
	}, func() string { return session })
	if h := do(override); h.Get("x-opencode-session") != "explicit" || h.Get("User-Agent") != "custom/1" {
		t.Fatalf("explicit override = %v", h)
	}

	session = "sess-two"
	if h := do(client); h.Get("x-opencode-session") != "sess-two" {
		t.Fatalf("session after /new = %q", h.Get("x-opencode-session"))
	}
}

func TestResolveHeadersCommand(t *testing.T) {
	got, err := (models.ModelEntry{Headers: map[string]string{"X-Token": "!printf tok"}}).ResolveHeaders()
	if err != nil {
		t.Fatal(err)
	}
	if got["X-Token"] != "tok" {
		t.Fatalf("resolved header = %q", got["X-Token"])
	}
	if _, err := (models.ModelEntry{Headers: map[string]string{"X-Token": "!exit 3"}}).ResolveHeaders(); err == nil {
		t.Fatal("expected error for failing command")
	} else if strings.Count(err.Error(), "\n") != 0 {
		t.Fatalf("error is not one line: %q", err)
	}
}
