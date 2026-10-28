package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func call(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "api-key dGVzdA==")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestGatewayAndConnectorLifecycle(t *testing.T) {
	s := NewStore(0, false)
	h := s.APIHandler()
	tn := "/api/tenantnetworks/1"

	if code, body := call(t, h, "POST", tn+"/multi-cloud-gateways", `{"name":"gw","tier":"FAST"}`); code != 400 || !strings.Contains(body, "not a valid tier") {
		t.Fatalf("bad tier: %d %s", code, body)
	}
	if code, body := call(t, h, "POST", tn+"/multi-cloud-gateways", `{"name":"gw","tier":"10GBPS","billingAccountNumber":"5-LMNCX0042"}`); code != 201 {
		t.Fatalf("create gateway: %d %s", code, body)
	}
	time.Sleep(time.Millisecond)
	if code, body := call(t, h, "GET", tn+"/multi-cloud-gateways?offset=0&limit=50", ""); code != 200 || !strings.Contains(body, `"hits": 1`) || !strings.Contains(body, "PROVISIONED") {
		t.Fatalf("list gateways: %d %s", code, body)
	}
	call(t, h, "POST", tn+"/segments", `{"name":"seg","asn":65513,"ipBlocks":{"values":["10.0.0.0/16"]}}`)

	conn := `{"name":"aws","cxp":"US-EAST","size":"SMALL","enabled":true,"multiCloudGatewayId":1,"loopbackPrefix":"100.64.10.0/26",` +
		`"segmentOptions":[{"segmentName":"seg"}],"instances":[{"name":"i1","onRampRegion":"%s"}]}`
	if code, body := call(t, h, "POST", tn+"/multi-cloud-gateway-connectors", strings.Replace(conn, "%s", "mars-1 - Nowhere", 1)); code != 400 || !strings.Contains(body, "not a supported Lumen on-ramp region") {
		t.Fatalf("bad region: %d %s", code, body)
	}
	if code, body := call(t, h, "POST", tn+"/multi-cloud-gateway-connectors", strings.Replace(conn, "%s", "us-east-1 - US East (N. Virginia) - Reston", 1)); code != 201 || !strings.Contains(body, "lumenConnectionId") {
		t.Fatalf("create connector: %d %s", code, body)
	}
	if code, body := call(t, h, "DELETE", tn+"/multi-cloud-gateways/1", ""); code != 400 || !strings.Contains(body, "still has connectors") {
		t.Fatalf("delete gateway with connectors: %d %s", code, body)
	}
	if code, body := call(t, h, "PUT", tn+"/multi-cloud-gateways/1", `{"name":"gw","tier":"50GBPS","billingAccountNumber":"5-OTHER0001"}`); code != 400 || !strings.Contains(body, "cannot be changed") {
		t.Fatalf("change BAN: %d %s", code, body)
	}
}

func TestSeededExternalGatewayIsReadOnly(t *testing.T) {
	h := NewStore(0, true).APIHandler()
	if code, body := call(t, h, "PUT", "/api/tenantnetworks/1/multi-cloud-gateways/1", `{"name":"lumen-connect-native-gw","tier":"10GBPS"}`); code != 400 || !strings.Contains(body, "read-only") {
		t.Fatalf("update external: %d %s", code, body)
	}
	if code, _ := call(t, h, "DELETE", "/api/tenantnetworks/1/multi-cloud-gateways/1", ""); code != 400 {
		t.Fatalf("delete external: %d", code)
	}
}

func TestRequiresAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/tenantnetworksummaries", nil)
	rec := httptest.NewRecorder()
	NewStore(0, false).APIHandler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("got %d", rec.Code)
	}
}
