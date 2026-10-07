package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

const releaseMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>com.hypixel.hytale</groupId>
  <artifactId>Server</artifactId>
  <versioning>
    <latest>0.6.8</latest>
    <release>0.6.8</release>
    <versions><version>0.6.7</version><version>0.6.8</version></versions>
    <lastUpdated>20260919040008</lastUpdated>
  </versioning>
</metadata>`

const preReleaseMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <versioning>
    <latest>0.7.0-pre.5.1</latest>
    <lastUpdated>20261002120000</lastUpdated>
  </versioning>
</metadata>`

func TestHytaleVersions_ReadsLatestPerPatchlineFromMaven(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release/com/hypixel/hytale/Server/maven-metadata.xml":
			_, _ = w.Write([]byte(releaseMetadata))
		case "/pre-release/com/hypixel/hytale/Server/maven-metadata.xml":
			_, _ = w.Write([]byte(preReleaseMetadata))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewHytaleVersionService(nil, srv.URL, zap.NewNop())
	got := svc.Versions(context.Background())

	if len(got.Patchlines) != 2 {
		t.Fatalf("got %d patchlines, want 2", len(got.Patchlines))
	}
	rel, pre := got.Patchlines[0], got.Patchlines[1]
	if rel.Patchline != "release" || rel.Latest != "0.6.8" || rel.UpdatedAt == "" {
		t.Fatalf("release = %+v", rel)
	}
	if pre.Patchline != "pre-release" || pre.Latest != "0.7.0-pre.5.1" {
		t.Fatalf("pre-release = %+v", pre)
	}
}

// Maven being down must not break server creation: the patchlines are still
// listed, just without a version number.
func TestHytaleVersions_UnreachableMavenStillListsPatchlines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer srv.Close()

	svc := NewHytaleVersionService(nil, srv.URL, zap.NewNop())
	got := svc.Versions(context.Background())

	if len(got.Patchlines) != 2 {
		t.Fatalf("got %d patchlines, want 2", len(got.Patchlines))
	}
	for _, p := range got.Patchlines {
		if p.Latest != "" {
			t.Fatalf("%s latest = %q, want empty on fetch failure", p.Patchline, p.Latest)
		}
	}
}

func TestNormalizeHytaleVersion(t *testing.T) {
	ok := map[string]string{
		"":            "latest",
		"latest":      "latest",
		"release":     "release",
		" Release ":   "release",
		"pre-release": "pre-release",
	}
	for in, want := range ok {
		got, err := NormalizeHytaleVersion(in)
		if err != nil || got != want {
			t.Errorf("NormalizeHytaleVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// The value is passed to the downloader as -patchline, which only knows
	// patchline names; a pinned build number would fail provisioning.
	for _, bad := range []string{"0.6.8", "1.0.0", "beta", "release; rm -rf"} {
		if _, err := NormalizeHytaleVersion(bad); err == nil {
			t.Errorf("NormalizeHytaleVersion(%q) accepted, want error", bad)
		}
	}
}
