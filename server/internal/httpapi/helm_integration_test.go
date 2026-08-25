package httpapi_test

import (
	"net/http"
	"testing"
)

type helmListBody struct {
	Releases []struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		Revision    int    `json:"revision"`
		Status      string `json:"status"`
		Chart       string `json:"chart"`
		Version     string `json:"chartVersion"`
		AppVersion  string `json:"appVersion"`
		Updated     string `json:"updated"`
		Description string `json:"description"`
	} `json:"releases"`
}

type helmDetailBody struct {
	Name    string         `json:"name"`
	Chart   string         `json:"chart"`
	Values  map[string]any `json:"values"`
	Notes   string         `json:"notes"`
	History []struct {
		Revision int    `json:"revision"`
		Status   string `json:"status"`
	} `json:"history"`
}

// Helm keeps a release as a Secret per revision, base64 twice and then gzipped. Reading
// it through the Kubernetes API rather than by running the helm binary means there is no
// second credential path and no dependence on which helm is on PATH.
func TestHelmReleasesAreReadFromTheCluster(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "helm-list")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/helm-releases", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list releases: %d %s", resp.StatusCode, readBody(resp))
	}

	releases := decode[helmListBody](t, resp).Releases
	if len(releases) == 0 {
		t.Skip("this cluster has no Helm releases installed")
	}

	for _, release := range releases {
		if release.Name == "" || release.Namespace == "" {
			t.Errorf("a release cannot be opened: %+v", release)
		}
		if release.Revision < 1 {
			t.Errorf("%s has revision %d", release.Name, release.Revision)
		}
		if release.Status == "" {
			t.Errorf("%s has no status", release.Name)
		}
		// The chart's identity lives in the payload rather than the labels, so this is
		// what proves the decode chain actually ran.
		if release.Chart == "" || release.Version == "" {
			t.Errorf("%s did not decode its chart: %+v", release.Name, release)
		}
	}
}

// Only the newest revision per release: a chart upgraded ten times is one installation,
// not ten.
func TestOnlyTheCurrentRevisionIsListed(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "helm-revisions")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/helm-releases", nil)
	releases := decode[helmListBody](t, resp).Releases
	_ = resp.Body.Close()

	seen := map[string]bool{}
	for _, release := range releases {
		key := release.Namespace + "/" + release.Name
		if seen[key] {
			t.Errorf("%s is listed more than once; revisions are not being collapsed", key)
		}
		seen[key] = true
	}
}

// What a release was installed with is the question people open one to answer.
func TestAReleaseCarriesItsValuesAndHistory(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "helm-detail")

	list := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/helm-releases", nil)
	releases := decode[helmListBody](t, list).Releases
	_ = list.Body.Close()

	if len(releases) == 0 {
		t.Skip("this cluster has no Helm releases installed")
	}
	first := releases[0]

	resp := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/helm-releases/"+first.Namespace+"/"+first.Name, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read release: %d %s", resp.StatusCode, readBody(resp))
	}

	detail := decode[helmDetailBody](t, resp)
	if detail.Name != first.Name {
		t.Fatalf("asked for %s, got %s", first.Name, detail.Name)
	}
	if detail.Chart == "" {
		t.Error("the release detail did not decode its chart")
	}
	if len(detail.History) == 0 {
		t.Error("a release with at least one revision reported no history")
	}
	for i := 1; i < len(detail.History); i++ {
		// Newest first: the current revision is the one being looked at.
		if detail.History[i-1].Revision < detail.History[i].Revision {
			t.Errorf("history is not newest-first: %+v", detail.History)
			break
		}
	}
}

func TestAskingForAReleaseThatIsNotThereSaysSo(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "helm-missing")

	resp := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/helm-releases/default/no-such-release", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("a release that does not exist was reported as found")
	}
}
