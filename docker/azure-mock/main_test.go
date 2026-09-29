package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const afdEndpointPath = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Cdn/profiles/shared-afd/afdEndpoints/shared-endpoint"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := NewServer()
	return httptest.NewServer(s)
}

func doJSON(t *testing.T, method, url string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest(method, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func TestAFDEndpointRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, created := doJSON(t, http.MethodPut, srv.URL+afdEndpointPath+"?api-version=2024-02-01",
		map[string]interface{}{"location": "global", "properties": map[string]interface{}{"enabledState": "Enabled"}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}
	props := created["properties"].(map[string]interface{})
	if props["hostName"] != "shared-endpoint-mock.z01.azurefd.net" {
		t.Fatalf("hostName = %v", props["hostName"])
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("provisioningState = %v", props["provisioningState"])
	}

	resp, got := doJSON(t, http.MethodGet, srv.URL+afdEndpointPath, nil)
	if resp.StatusCode != http.StatusOK || got["name"] != "shared-endpoint" {
		t.Fatalf("GET status = %d name = %v", resp.StatusCode, got["name"])
	}

	resp, _ = doJSON(t, http.MethodDelete, srv.URL+afdEndpointPath, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE status = %d", resp.StatusCode)
	}
	resp, _ = doJSON(t, http.MethodGet, srv.URL+afdEndpointPath, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after DELETE status = %d", resp.StatusCode)
	}
}

func TestAFDCustomDomainExposesValidationToken(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	path := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Cdn/profiles/shared-afd/customDomains/app-domain"
	_, created := doJSON(t, http.MethodPut, srv.URL+path,
		map[string]interface{}{"properties": map[string]interface{}{"hostName": "app.example.com"}})
	props := created["properties"].(map[string]interface{})
	vp, ok := props["validationProperties"].(map[string]interface{})
	if !ok || vp["validationToken"] == "" {
		t.Fatalf("validationProperties missing: %v", props)
	}
	if props["domainValidationState"] != "Approved" {
		t.Fatalf("domainValidationState = %v", props["domainValidationState"])
	}
}

func TestAFDPurgeIsRecorded(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	doJSON(t, http.MethodPut, srv.URL+afdEndpointPath, map[string]interface{}{"location": "global"})
	resp, _ := doJSON(t, http.MethodPost, srv.URL+afdEndpointPath+"/purge?api-version=2025-04-15",
		map[string]interface{}{"contentPaths": []string{"/*"}, "domains": []string{"app.example.com"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("purge status = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mock/afd/purges", nil)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var purges []map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&purges)
	r.Body.Close()
	if len(purges) != 1 {
		t.Fatalf("purges = %d", len(purges))
	}
	domains := purges[0]["domains"].([]interface{})
	if domains[0] != "app.example.com" {
		t.Fatalf("domains = %v", domains)
	}
}

func TestDNSTXTRecordRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	path := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/dnszones/example.com/TXT/_dnsauth.app"
	resp, created := doJSON(t, http.MethodPut, srv.URL+path,
		map[string]interface{}{"properties": map[string]interface{}{"TTL": 3600,
			"TXTRecords": []map[string]interface{}{{"value": []string{"token"}}}}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}
	if created["properties"].(map[string]interface{})["fqdn"] != "_dnsauth.app.example.com." {
		t.Fatalf("fqdn = %v", created["properties"])
	}
}

func TestAFDRuleSetAcceptsEmptyBody(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	path := srv.URL + "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Cdn/profiles/shared-afd/ruleSets/rules1"
	req, _ := http.NewRequest(http.MethodPut, path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	resp, got := doJSON(t, http.MethodGet, path, nil)
	if resp.StatusCode != http.StatusOK || got["name"] != "rules1" {
		t.Fatalf("GET status = %d name = %v", resp.StatusCode, got["name"])
	}
	if got["properties"].(map[string]interface{})["provisioningState"] != "Succeeded" {
		t.Fatalf("properties = %v", got["properties"])
	}
}
