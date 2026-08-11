// GCP Mock API Server
//
// A lightweight mock of the subset of Google Cloud REST APIs used by the
// gcp-functions scope, for integration testing without real GCP resources:
//
//   - Compute (global + regional): addresses, sslCertificates, backendServices,
//     urlMaps, targetHttpsProxies, globalForwardingRules, regionNetworkEndpointGroups,
//     and their long-running operations
//   - Cloud Functions gen2: functions + operations (google.longrunning style)
//   - Cloud Run: service getIamPolicy / setIamPolicy (invoker bindings)
//   - Cloud DNS: managed-zone changes + record-set listing
//   - IAM: service accounts
//
// Object storage is handled by a separate fake-gcs-server container, not here.
//
// The provider is pointed here via *_custom_endpoint overrides (see
// gcp-mock-provider/provider_override.tf) with a static access token, so no OAuth
// token exchange is needed.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

// =============================================================================
// In-memory store
// =============================================================================

type Store struct {
	mu         sync.Mutex
	resources  map[string]map[string]interface{} // key: canonical GET path
	operations map[string]map[string]interface{} // key: operation id -> target resource path
	seq        int
}

func NewStore() *Store {
	return &Store{
		resources:  map[string]map[string]interface{}{},
		operations: map[string]map[string]interface{}{},
	}
}

func (s *Store) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

// nextNum returns a monotonically increasing integer, used for numeric GCP resource IDs
// (compute resources expose uint64 ids; the provider parses several of them as ints).
func (s *Store) nextNum() int {
	s.seq++
	return s.seq
}

// =============================================================================
// Server
// =============================================================================

type Server struct {
	store *Store
}

func NewServer() *Server { return &Server{store: NewStore()} }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	log.Printf("%s %s", r.Method, path)

	if path == "/health" || path == "/" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch {
	case strings.HasPrefix(path, "/compute/"):
		s.handleCompute(w, r)
	case strings.HasPrefix(path, "/cloudfunctions/"):
		s.handleFunctions(w, r)
	case strings.HasPrefix(path, "/run/"):
		s.handleRun(w, r)
	case strings.HasPrefix(path, "/dns/"):
		s.handleDNS(w, r)
	case strings.HasPrefix(path, "/iam/"):
		s.handleIAM(w, r)
	default:
		s.notFound(w, path)
	}
}

// =============================================================================
// Compute (LRO-style operations)
// =============================================================================

func (s *Server) handleCompute(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Operation polling: always DONE.
	if strings.Contains(path, "/operations/") {
		id := lastSegment(path)
		s.store.mu.Lock()
		op := s.store.operations[id]
		s.store.mu.Unlock()
		if op == nil {
			op = map[string]interface{}{"name": id, "status": "DONE", "progress": 100}
		}
		writeJSON(w, http.StatusOK, op)
		return
	}

	switch r.Method {
	case http.MethodPost:
		body := readJSON(r)
		name, _ := body["name"].(string)
		key := path + "/" + name
		injectComputeComputed(collectionName(path), name, body, r.Host, key, s.store)
		s.store.put(key, body)
		writeJSON(w, http.StatusOK, s.newComputeOp(r.Host, parentPath(path), key))
	case http.MethodGet:
		if res, ok := s.store.get(path); ok {
			writeJSON(w, http.StatusOK, res)
			return
		}
		// Collection listing (unknown item still 404 so tofu treats it as absent).
		s.notFound(w, path)
	case http.MethodDelete:
		s.store.del(path)
		writeJSON(w, http.StatusOK, s.newComputeOp(r.Host, parentOfItem(path), path))
	case http.MethodPatch, http.MethodPut:
		body := readJSON(r)
		s.store.merge(path, body)
		writeJSON(w, http.StatusOK, s.newComputeOp(r.Host, parentOfItem(path), path))
	default:
		s.notFound(w, path)
	}
}

func (s *Server) newComputeOp(host, parent, targetKey string) map[string]interface{} {
	id := s.store.nextID("operation")
	selfLink := fmt.Sprintf("http://%s%s/operations/%s", host, parent, id)
	op := map[string]interface{}{
		"kind":       "compute#operation",
		"name":       id,
		"status":     "DONE",
		"progress":   100,
		"selfLink":   selfLink,
		"targetLink": fmt.Sprintf("http://%s%s", host, targetKey),
	}
	s.store.mu.Lock()
	s.store.operations[id] = op
	s.store.mu.Unlock()
	return op
}

// injectComputeComputed fills the computed fields the provider reads back after create.
func injectComputeComputed(collection, name string, body map[string]interface{}, host, key string, store *Store) {
	// Compute resource ids are numeric (uint64) strings; the provider parses several of
	// them (e.g. certificate_id) as ints, so a non-numeric id crashes it.
	body["id"] = fmt.Sprintf("%d", 100000000+store.nextNum())
	body["selfLink"] = fmt.Sprintf("http://%s%s", host, key)
	body["creationTimestamp"] = "2024-01-01T00:00:00Z"
	// A global address must expose an allocated IP; use a deterministic one so the DNS
	// record-set assertion is stable.
	if collection == "addresses" {
		body["address"] = "203.0.113.42"
		body["status"] = "RESERVED"
	}
}

// =============================================================================
// Cloud Functions gen2 (google.longrunning operations)
// =============================================================================

func (s *Server) handleFunctions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.Contains(path, "/operations/") {
		id := lastSegment(path)
		s.store.mu.Lock()
		op := s.store.operations[id]
		s.store.mu.Unlock()
		if op == nil {
			op = map[string]interface{}{"name": path, "done": true}
		}
		writeJSON(w, http.StatusOK, op)
		return
	}

	switch r.Method {
	case http.MethodPost:
		body := readJSON(r)
		functionID := r.URL.Query().Get("functionId")
		key := path + "/" + functionID
		resourceName := strings.TrimPrefix(key, "/cloudfunctions/v2/")
		fn := enrichFunction(body, resourceName, functionID, r.Host)
		s.store.put(key, fn)
		writeJSON(w, http.StatusOK, s.newLRO(parentPath(path), fn))
	case http.MethodGet:
		if res, ok := s.store.get(path); ok {
			writeJSON(w, http.StatusOK, res)
			return
		}
		s.notFound(w, path)
	case http.MethodPatch, http.MethodPut:
		body := readJSON(r)
		s.store.merge(path, body)
		res, _ := s.store.get(path)
		writeJSON(w, http.StatusOK, s.newLRO(parentOfItem(path), res))
	case http.MethodDelete:
		s.store.del(path)
		writeJSON(w, http.StatusOK, s.newLRO(parentOfItem(path), map[string]interface{}{}))
	default:
		s.notFound(w, path)
	}
}

func enrichFunction(body map[string]interface{}, resourceName, functionID, host string) map[string]interface{} {
	body["name"] = resourceName
	body["state"] = "ACTIVE"
	sc, ok := body["serviceConfig"].(map[string]interface{})
	if !ok {
		sc = map[string]interface{}{}
	}
	sc["uri"] = fmt.Sprintf("https://%s-mockproj.a.run.app", functionID)
	sc["service"] = functionID
	body["serviceConfig"] = sc
	return body
}

func (s *Server) newLRO(parent string, response map[string]interface{}) map[string]interface{} {
	id := s.store.nextID("operation")
	opName := strings.TrimPrefix(parent, "/cloudfunctions/v2/") + "/operations/" + id
	resp := map[string]interface{}{}
	for k, v := range response {
		resp[k] = v
	}
	resp["@type"] = "type.googleapis.com/google.cloud.functions.v2.Function"
	op := map[string]interface{}{
		"name":     opName,
		"done":     true,
		"response": resp,
	}
	s.store.mu.Lock()
	s.store.operations[id] = op
	s.store.mu.Unlock()
	return op
}

// =============================================================================
// Cloud Run — IAM policy (invoker bindings)
// =============================================================================

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case strings.HasSuffix(path, ":getIamPolicy"):
		key := strings.TrimSuffix(path, ":getIamPolicy") + "#iam"
		if pol, ok := s.store.get(key); ok {
			writeJSON(w, http.StatusOK, pol)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"version": 1, "etag": "ACAB", "bindings": []interface{}{}})
	case strings.HasSuffix(path, ":setIamPolicy"):
		body := readJSON(r)
		key := strings.TrimSuffix(path, ":setIamPolicy") + "#iam"
		policy, _ := body["policy"].(map[string]interface{})
		if policy == nil {
			policy = map[string]interface{}{}
		}
		policy["etag"] = "ACAB"
		s.store.put(key, policy)
		writeJSON(w, http.StatusOK, policy)
	default:
		s.notFound(w, path)
	}
}

// =============================================================================
// Cloud DNS — managed-zone changes + record-set listing
// =============================================================================

func (s *Server) handleDNS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// POST /dns/v1/projects/{p}/managedZones/{z}/changes
	if strings.HasSuffix(path, "/changes") && r.Method == http.MethodPost {
		body := readJSON(r)
		zonePath := strings.TrimSuffix(path, "/changes")
		if adds, ok := body["additions"].([]interface{}); ok {
			for _, a := range adds {
				rr, _ := a.(map[string]interface{})
				name, _ := rr["name"].(string)
				typ, _ := rr["type"].(string)
				s.store.put(zonePath+"/rrsets/"+name+"/"+typ, rr)
			}
		}
		if dels, ok := body["deletions"].([]interface{}); ok {
			for _, d := range dels {
				rr, _ := d.(map[string]interface{})
				name, _ := rr["name"].(string)
				typ, _ := rr["type"].(string)
				s.store.del(zonePath + "/rrsets/" + name + "/" + typ)
			}
		}
		id := s.store.nextID("change")
		body["kind"] = "dns#change"
		body["status"] = "done"
		body["id"] = id
		// Store so the provider's GET .../changes/{id} poll succeeds.
		s.store.put(path+"/"+id, body)
		writeJSON(w, http.StatusOK, body)
		return
	}

	// GET /dns/v1/projects/{p}/managedZones/{z}/changes/{id} (change status poll)
	if strings.Contains(path, "/changes/") && r.Method == http.MethodGet {
		if change, ok := s.store.get(path); ok {
			change["status"] = "done"
			writeJSON(w, http.StatusOK, change)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"kind": "dns#change", "status": "done", "id": lastSegment(path)})
		return
	}

	// GET /dns/v1/projects/{p}/managedZones/{z}/rrsets?name=&type=
	if strings.HasSuffix(path, "/rrsets") && r.Method == http.MethodGet {
		name := r.URL.Query().Get("name")
		typ := r.URL.Query().Get("type")
		key := path + "/" + name + "/" + typ
		rrsets := []interface{}{}
		if rr, ok := s.store.get(key); ok {
			rrsets = append(rrsets, rr)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"kind": "dns#resourceRecordSetsListResponse", "rrsets": rrsets})
		return
	}

	s.notFound(w, path)
}

// =============================================================================
// IAM — service accounts
// =============================================================================

func (s *Server) handleIAM(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch r.Method {
	case http.MethodPost:
		// POST /iam/v1/projects/{p}/serviceAccounts { accountId, serviceAccount }
		body := readJSON(r)
		project := projectFromIAMPath(path)
		accountID, _ := body["accountId"].(string)
		email := fmt.Sprintf("%s@%s.iam.gserviceaccount.com", accountID, project)
		sa := map[string]interface{}{
			"name":        fmt.Sprintf("projects/%s/serviceAccounts/%s", project, email),
			"email":       email,
			"projectId":   project,
			"uniqueId":    s.store.nextID("sa"),
			"displayName": stringField(body, "serviceAccount", "displayName"),
		}
		s.store.put(path+"/"+email, sa)
		writeJSON(w, http.StatusOK, sa)
	case http.MethodGet:
		if res, ok := s.store.get(path); ok {
			writeJSON(w, http.StatusOK, res)
			return
		}
		s.notFound(w, path)
	case http.MethodDelete:
		s.store.del(path)
		writeJSON(w, http.StatusOK, map[string]interface{}{})
	default:
		s.notFound(w, path)
	}
}

// =============================================================================
// Store helpers
// =============================================================================

func (s *Store) put(key string, v map[string]interface{}) {
	s.mu.Lock()
	s.resources[key] = v
	s.mu.Unlock()
}

func (s *Store) get(key string) (map[string]interface{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.resources[key]
	return v, ok
}

func (s *Store) del(key string) {
	s.mu.Lock()
	delete(s.resources, key)
	s.mu.Unlock()
}

func (s *Store) merge(key string, patch map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.resources[key]
	if !ok {
		s.resources[key] = patch
		return
	}
	for k, v := range patch {
		cur[k] = v
	}
}

// =============================================================================
// Small helpers
// =============================================================================

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request) map[string]interface{} {
	out := map[string]interface{}{}
	b, err := io.ReadAll(r.Body)
	if err != nil || len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Server) notFound(w http.ResponseWriter, path string) {
	writeJSON(w, http.StatusNotFound, map[string]interface{}{
		"error": map[string]interface{}{
			"code":    404,
			"message": fmt.Sprintf("mock: not found: %s", path),
			"status":  "NOT_FOUND",
		},
	})
}

func lastSegment(path string) string {
	parts := strings.Split(strings.TrimRight(path, "/"), "/")
	return parts[len(parts)-1]
}

// collectionName returns the last path segment of a collection path (POST target).
func collectionName(path string) string { return lastSegment(path) }

// parentPath strips the final segment (used for POST collection paths).
func parentPath(path string) string {
	idx := strings.LastIndex(strings.TrimRight(path, "/"), "/")
	if idx <= 0 {
		return path
	}
	return path[:idx]
}

// parentOfItem strips the item name AND its collection (used for item GET/DELETE paths).
func parentOfItem(path string) string {
	return parentPath(parentPath(path))
}

func projectFromIAMPath(path string) string {
	// /iam/v1/projects/{p}/serviceAccounts...
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return "mock-project"
}

func stringField(m map[string]interface{}, keys ...string) string {
	var cur interface{} = m
	for _, k := range keys {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

func main() {
	server := NewServer()
	log.Println("GCP Mock API Server")
	log.Println("===================")
	log.Println("  Compute:        /compute/v1/projects/{p}/(global|regions/{r})/...")
	log.Println("  Functions gen2: /cloudfunctions/v2/projects/{p}/locations/{l}/functions")
	log.Println("  Cloud Run IAM:  /run/v1/projects/{p}/locations/{l}/services/{s}:[get|set]IamPolicy")
	log.Println("  Cloud DNS:      /dns/v1/projects/{p}/managedZones/{z}/changes")
	log.Println("  IAM:            /iam/v1/projects/{p}/serviceAccounts")
	log.Println("Listening on :8080 ...")
	if err := http.ListenAndServe(":8080", server); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
