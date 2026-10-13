package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ipxemultiboot/internal/api"
	"ipxemultiboot/internal/app"
	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/library"
	"ipxemultiboot/internal/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *client) do(method, path, body string, csrf bool) (int, map[string]any) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if csrf {
		req.Header.Set("X-IPXEMultiboot", "1")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func setup(t *testing.T) (*client, string) {
	t.Helper()
	dir := t.TempDir()
	isoDir := filepath.Join(dir, "isos")
	st, err := store.Open(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &app.App{
		Version:  "test",
		HTTPPort: 8080,
		Defaults: app.Settings{ISODir: isoDir, DHCPMode: "proxy", Language: "en", Theme: "system"},
		Store:    st,
		Lib:      library.New(isoDir),
		Hub:      events.NewHub(10),
	}
	if err := a.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api.New(a).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}, isoDir
}

func TestRegistrationFlow(t *testing.T) {
	c, _ := setup(t)

	if code, body := c.do("GET", "/api/setup/status", "", false); code != 200 || body["needsRegistration"] != true {
		t.Fatalf("status: %d %v", code, body)
	}
	if code, _ := c.do("GET", "/api/settings", "", true); code != 401 {
		t.Errorf("settings without login = %d, want 401", code)
	}
	if code, _ := c.do("POST", "/api/setup/register", `{"username":"admin","password":"short"}`, true); code != 400 {
		t.Errorf("short password = %d, want 400", code)
	}
	if code, _ := c.do("POST", "/api/setup/register", `{"username":"admin","password":"longenough1"}`, false); code != 403 {
		t.Errorf("register without CSRF header = %d, want 403", code)
	}
	if code, _ := c.do("POST", "/api/setup/register", `{"username":"admin","password":"longenough1"}`, true); code != 201 {
		t.Fatalf("register = %d", code)
	}
	if code, _ := c.do("POST", "/api/setup/register", `{"username":"evil","password":"longenough1"}`, true); code != 409 {
		t.Errorf("second register = %d, want 409", code)
	}
	// Registration logs in.
	if code, body := c.do("GET", "/api/auth/me", "", false); code != 200 || body["username"] != "admin" {
		t.Errorf("me = %d %v", code, body)
	}
	if code, _ := c.do("PUT", "/api/settings", `{"theme":"dark"}`, false); code != 403 {
		t.Errorf("mutation without CSRF header = %d, want 403", code)
	}

	c.do("POST", "/api/auth/logout", "", true)
	if code, _ := c.do("GET", "/api/auth/me", "", false); code != 401 {
		t.Errorf("me after logout = %d, want 401", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"username":"admin","password":"wrong-password"}`, true); code != 401 {
		t.Errorf("bad login = %d, want 401", code)
	}
	if code, _ := c.do("POST", "/api/auth/login", `{"username":"ADMIN","password":"longenough1"}`, true); code != 200 {
		t.Errorf("login (case-insensitive username) = %d", code)
	}
}

func TestSettings(t *testing.T) {
	c, _ := setup(t)
	c.do("POST", "/api/setup/register", `{"username":"admin","password":"longenough1"}`, true)

	code, body := c.do("PUT", "/api/settings", `{"theme":"dark","language":"de","dhcpMode":"bootstrap"}`, true)
	if code != 200 || body["theme"] != "dark" || body["language"] != "de" || body["dhcpMode"] != "bootstrap" {
		t.Fatalf("save = %d %v", code, body)
	}
	if code, body := c.do("PUT", "/api/settings", `{"dhcpMode":"full","poolStart":"nope"}`, true); code != 400 {
		t.Errorf("invalid pool accepted: %d %v", code, body)
	}
	if code, _ := c.do("PUT", "/api/settings", `{"language":"xx"}`, true); code != 400 {
		t.Errorf("unknown language accepted")
	}
	// Partial updates keep other fields.
	if _, body := c.do("GET", "/api/settings", "", false); body["theme"] != "dark" || body["dhcpMode"] != "bootstrap" {
		t.Errorf("settings not persisted: %v", body)
	}
	if code, body := c.do("POST", "/api/setup/complete", "", true); code != 200 || body["setupComplete"] != true {
		t.Errorf("complete = %d %v", code, body)
	}
}

func TestUploadAndDelete(t *testing.T) {
	c, isoDir := setup(t)
	c.do("POST", "/api/setup/register", `{"username":"admin","password":"longenough1"}`, true)

	upload := func(name string) int {
		req, _ := http.NewRequest("PUT", c.base+"/api/isos/upload?name="+name, strings.NewReader("not really an iso"))
		req.Header.Set("X-IPXEMultiboot", "1")
		res, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := upload("test.iso"); code != 201 {
		t.Fatalf("upload = %d", code)
	}
	if _, err := os.Stat(filepath.Join(isoDir, "test.iso")); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
	if code := upload("test.iso"); code != 409 {
		t.Errorf("duplicate upload = %d, want 409", code)
	}
	for _, bad := range []string{"..%2Fescape.iso", "notes.txt", ".hidden.iso", "a%2Fb.iso"} {
		if code := upload(bad); code != 400 {
			t.Errorf("upload %q = %d, want 400", bad, code)
		}
	}
	entries, _ := os.ReadDir(isoDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}

	req, _ := http.NewRequest("GET", c.base+"/api/isos", nil)
	res, _ := c.http.Do(req)
	var isos []map[string]any
	json.NewDecoder(res.Body).Decode(&isos)
	res.Body.Close()
	if len(isos) != 1 || isos[0]["boot"] != "sanboot" {
		t.Fatalf("isos = %v", isos)
	}
	if code, _ := c.do("DELETE", "/api/isos/"+isos[0]["id"].(string), "", true); code != 204 {
		t.Errorf("delete = %d", code)
	}
	if _, err := os.Stat(filepath.Join(isoDir, "test.iso")); !os.IsNotExist(err) {
		t.Error("file still exists after delete")
	}
}
