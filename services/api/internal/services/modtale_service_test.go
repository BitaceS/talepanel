package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

const (
	mtPluginID = "49fee526-166c-418f-b4cc-01e5cbbf3776"
	mtWorldID  = "11111111-2222-3333-4444-555555555555"
)

// fakeModtale serves the parts of the Modtale API the panel uses, shaped like
// the real responses (checked against api.modtale.net on 2026-11-20).
type fakeModtale struct {
	srv          *httptest.Server
	calls        atomic.Int32
	downloadPath string // what download-url answers with
}

func newFakeModtale(t *testing.T) *fakeModtale {
	t.Helper()
	f := &fakeModtale{downloadPath: "/download/tok_123"}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("User-Agent") == "" || !strings.HasPrefix(r.Header.Get("User-Agent"), "TalePanel/") {
			t.Errorf("missing TalePanel User-Agent: %q", r.Header.Get("User-Agent"))
		}
		write(w, map[string]any{
			"content": []map[string]any{
				{"id": mtPluginID, "title": "HytaleMetrics", "description": "Metrics", "author": "HytaleCore",
					"imageUrl": "https://cdn.modtale.net/images/x.png", "classification": "PLUGIN",
					"downloadCount": 13655, "updatedAt": "2026-06-03T16:04:49"},
				{"id": "../../etc", "title": "bad id"},
				{"id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "title": "plain http icon", "imageUrl": "http://evil/x.png", "classification": "PLUGIN"},
			},
			"totalElements": 2, "number": 0,
			"echo": r.URL.RawQuery,
		})
	})
	mux.HandleFunc("/api/v1/projects/"+mtPluginID, func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		write(w, map[string]any{"id": mtPluginID, "title": "HytaleMetrics", "classification": "PLUGIN"})
	})
	mux.HandleFunc("/api/v1/projects/"+mtPluginID+"/versions", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		write(w, map[string]any{"versions": []map[string]any{
			{"id": "v4", "versionNumber": "1.4.0-beta", "channel": "BETA", "gameVersions": []string{"0.6.0"},
				"fileUrl": "files/plugin/7c5ac823-9ccc-4a73-94dc-832baf40e18d-HytaleMetrics-1.4.0-beta.jar", "releaseDate": "2026-06-04"},
			{"id": "v3", "versionNumber": "1.3.0", "channel": "RELEASE", "gameVersions": []string{"0.5.3"},
				"fileUrl": "files/plugin/7c5ac823-9ccc-4a73-94dc-832baf40e18d-HytaleMetrics-1.3.0.jar", "releaseDate": "2026-06-03"},
			{"id": "v2", "versionNumber": "1.2.0", "channel": "RELEASE",
				"fileUrl": "files/plugin/../../evil.sh", "releaseDate": "2026-05-01"},
		}})
	})
	mux.HandleFunc("/api/v1/projects/"+mtPluginID+"/versions/", func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/download-url") {
			http.NotFound(w, r)
			return
		}
		write(w, map[string]any{"downloadUrl": f.downloadPath, "expiresIn": 300})
	})
	mux.HandleFunc("/api/v1/projects/"+mtWorldID, func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"id": mtWorldID, "title": "A world", "classification": "SAVE"})
	})
	mux.HandleFunc("/api/v1/projects/"+mtWorldID+"/versions", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"versions": []map[string]any{
			{"id": "w1", "versionNumber": "1.0", "channel": "RELEASE", "fileUrl": "files/save/7c5ac823-9ccc-4a73-94dc-832baf40e18d-World.zip"},
		}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeModtale) service() *ModtaleService {
	s := NewModtaleService("")
	s.base = f.srv.URL + "/api/v1"
	return s
}

func TestModtaleFileName(t *testing.T) {
	cases := map[string]string{
		"files/plugin/7c5ac823-9ccc-4a73-94dc-832baf40e18d-HytaleMetrics-Hytale-1.3.0.jar": "HytaleMetrics-Hytale-1.3.0.jar",
		"files/data/279e35f2-f54a-4039-b56f-4fdd79b5f025-Dub_Cannabinol_Update_1.1.1.zip":  "Dub_Cannabinol_Update_1.1.1.zip",
		"files/plugin/Plain.jar":                                        "Plain.jar",
		"files/plugin/../../evil.sh":                                    "",
		"files/plugin/7c5ac823-9ccc-4a73-94dc-832baf40e18d-.hidden.jar": "",
		"files/plugin/name;rm -rf.jar":                                  "",
		"files/plugin/x.exe":                                            "",
		"":                                                              "",
	}
	for in, want := range cases {
		if got := modtaleFileName(in); got != want {
			t.Errorf("modtaleFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModtaleSearchMapsAndCaches(t *testing.T) {
	f := newFakeModtale(t)
	s := f.service()
	ctx := context.Background()

	res, err := s.Search(ctx, "metrics", "plugin", "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 2 {
		t.Fatalf("got %d projects, want 2 (the one with a bad ID dropped)", len(res.Projects))
	}
	p := res.Projects[0]
	if p.ID != mtPluginID || p.Title != "HytaleMetrics" || p.Downloads != 13655 || p.Author != "HytaleCore" {
		t.Fatalf("unexpected mapping: %+v", p)
	}
	if p.PageURL != "https://modtale.net/mod/"+mtPluginID {
		t.Fatalf("page url %q", p.PageURL)
	}
	if res.Projects[1].ImageURL != "" {
		t.Fatalf("plain-http image kept: %q", res.Projects[1].ImageURL)
	}

	before := f.calls.Load()
	if _, err := s.Search(ctx, "metrics", "PLUGIN", "relevance", 0, 20); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != before {
		t.Fatal("identical search was not served from the cache")
	}
}

func TestModtaleSearchNormalisesParams(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"content":[],"totalElements":0,"number":0}`))
	}))
	defer srv.Close()
	s := NewModtaleService("k")
	s.base = srv.URL + "/api/v1"
	if _, err := s.Search(context.Background(), "", "SAVE", "nonsense", -3, 1000); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"classification=PLUGIN", "sort=downloads", "page=0", "size=20"} {
		if !strings.Contains(got, want) {
			t.Errorf("query %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "search=") {
		t.Errorf("empty search sent: %q", got)
	}
}

func TestModtaleResolveDownload(t *testing.T) {
	f := newFakeModtale(t)
	s := f.service()
	ctx := context.Background()

	dl, err := s.ResolveDownload(ctx, mtPluginID, "")
	if err != nil {
		t.Fatal(err)
	}
	if dl.Version != "1.3.0" || dl.FileName != "HytaleMetrics-1.3.0.jar" {
		t.Fatalf("latest release: got %s / %s", dl.Version, dl.FileName)
	}
	if dl.URL != f.srv.URL+"/api/v1/download/tok_123" {
		t.Fatalf("download url %q", dl.URL)
	}

	dl, err = s.ResolveDownload(ctx, mtPluginID, "1.4.0-beta")
	if err != nil || dl.FileName != "HytaleMetrics-1.4.0-beta.jar" {
		t.Fatalf("explicit version: %+v, %v", dl, err)
	}
	// 1.2.0 has an unsafe file name, so it is not offered at all.
	if _, err := s.ResolveDownload(ctx, mtPluginID, "1.2.0"); !errors.Is(err, ErrModtaleNotFound) {
		t.Fatalf("unsafe version: err=%v", err)
	}
	if _, err := s.ResolveDownload(ctx, mtPluginID, "9.9.9"); !errors.Is(err, ErrModtaleNotFound) {
		t.Fatalf("missing version: err=%v", err)
	}
	if _, err := s.ResolveDownload(ctx, mtWorldID, ""); !errors.Is(err, ErrModtaleNotInstallable) {
		t.Fatalf("world: err=%v", err)
	}
	if _, err := s.ResolveDownload(ctx, "../admin", ""); !errors.Is(err, ErrModtaleInvalidID) {
		t.Fatalf("bad id: err=%v", err)
	}
}

func TestModtaleRefusesForeignDownloadLinks(t *testing.T) {
	for _, bad := range []string{
		"https://evil.example/download/x",
		"//evil.example/download/x",
		"/admin/download/x",
		"/download/",
		"/download/a/b",
	} {
		f := newFakeModtale(t)
		f.downloadPath = bad
		if _, err := f.service().ResolveDownload(context.Background(), mtPluginID, ""); err == nil {
			t.Errorf("download link %q accepted", bad)
		}
	}
}

func TestModtaleUpstreamErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	s := NewModtaleService("")
	s.base = srv.URL + "/api/v1"
	_, err := s.Search(context.Background(), "x", "", "", 0, 10)
	var mtErr *ModtaleError
	if !errors.As(err, &mtErr) || mtErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err=%v, want ModtaleError 429", err)
	}
}

// A user without admin rights installs from Modtale: the panel picks file name
// and link, stores the project, and enqueues the signed link for the node.
func TestInstallModFromModtale(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	f := newFakeModtale(t)
	svc := NewModService(pool)
	svc.SetModtale(f.service())

	owner := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, owner)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})

	// Client-supplied filename and URL are ignored for Modtale installs.
	m, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{
		ModtaleProjectID: mtPluginID,
		Filename:         "../../evil.jar",
		DownloadURL:      "https://attacker.example/x.jar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Filename != "HytaleMetrics-1.3.0.jar" || m.Version != "1.3.0" || m.Source != "modtale" ||
		m.DisplayName != "HytaleMetrics" || m.ModtaleProjectID == nil || *m.ModtaleProjectID != mtPluginID {
		t.Fatalf("unexpected row: %+v", m)
	}
	if m.DownloadURL != "https://modtale.net/mod/"+mtPluginID {
		t.Fatalf("stored url %q, want the project page", m.DownloadURL)
	}

	var payload map[string]any
	if err := pool.QueryRow(ctx, `
		SELECT payload FROM node_commands WHERE server_id = $1 AND command_type = 'install_mod'
		ORDER BY created_at DESC LIMIT 1`, serverID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["download_url"] != f.srv.URL+"/api/v1/download/tok_123" || payload["filename"] != "HytaleMetrics-1.3.0.jar" {
		t.Fatalf("payload %+v", payload)
	}

	mods, err := svc.ListMods(ctx, serverID)
	if err != nil || len(mods) != 1 || mods[0].ModtaleProjectID == nil {
		t.Fatalf("ListMods: %+v, %v", mods, err)
	}

	// Switch to the beta: old file removed, new one installed, project kept.
	if err := svc.SwitchModVersion(ctx, models.RoleUser, serverID, "HytaleMetrics-1.3.0.jar",
		ModVersionSwitchRequest{ModtaleVersion: "1.4.0-beta"}); err != nil {
		t.Fatal(err)
	}
	mods, _ = svc.ListMods(ctx, serverID)
	if len(mods) != 1 || mods[0].Filename != "HytaleMetrics-1.4.0-beta.jar" || mods[0].Version != "1.4.0-beta" ||
		mods[0].Source != "modtale" || mods[0].DisplayName != "HytaleMetrics" {
		t.Fatalf("after switch: %+v", mods)
	}
	var removes int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM node_commands WHERE server_id = $1 AND command_type = 'remove_mod'
		  AND payload->>'filename' = 'HytaleMetrics-1.3.0.jar'`, serverID).Scan(&removes); err != nil {
		t.Fatal(err)
	}
	if removes != 1 {
		t.Fatalf("%d remove commands for the old file, want 1", removes)
	}

	// A world is refused and nothing is stored.
	if _, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{ModtaleProjectID: mtWorldID}); !errors.Is(err, ErrModtaleNotInstallable) {
		t.Fatalf("world install err=%v", err)
	}
	// A CurseForge mod cannot be switched through Modtale.
	if _, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{
		Filename: "cf.jar", DownloadURL: "https://edge.forgecdn.net/files/1/2/cf.jar", CFModID: ptrInt(5),
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SwitchModVersion(ctx, models.RoleUser, serverID, "cf.jar",
		ModVersionSwitchRequest{ModtaleVersion: "1.3.0"}); !errors.Is(err, ErrModRequestInvalid) {
		t.Fatalf("switch of a CurseForge mod via Modtale: err=%v", err)
	}
	if _, err := svc.InstallMod(ctx, models.RoleUser, serverID, InstallModRequest{}); !errors.Is(err, ErrModRequestInvalid) {
		t.Fatalf("empty request err=%v", err)
	}
}

func ptrInt(v int) *int { return &v }
