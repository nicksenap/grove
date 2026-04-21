package mcp

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalRequest_Request(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req, ok := unmarshalRequest(data)
	if !ok {
		t.Fatal("unmarshalRequest returned !ok for valid input")
	}
	if req.Method != "initialize" {
		t.Errorf("Method = %q, want %q", req.Method, "initialize")
	}
	if req.isNotification() {
		t.Error("expected request, got notification")
	}
	if string(req.ID) != "1" {
		t.Errorf("ID raw = %q, want %q", string(req.ID), "1")
	}
}

func TestUnmarshalRequest_NullID(t *testing.T) {
	// id is present but null — spec says this is still a request.
	data := []byte(`{"jsonrpc":"2.0","id":null,"method":"ping"}`)
	req, ok := unmarshalRequest(data)
	if !ok {
		t.Fatal("unmarshalRequest returned !ok")
	}
	if req.isNotification() {
		t.Error("id:null should be a request, not a notification")
	}
}

func TestUnmarshalRequest_Notification(t *testing.T) {
	// Absent id — notification.
	data := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	req, ok := unmarshalRequest(data)
	if !ok {
		t.Fatal("unmarshalRequest returned !ok")
	}
	if !req.isNotification() {
		t.Error("expected notification, got request")
	}
}

func TestUnmarshalRequest_MalformedJSON(t *testing.T) {
	req, ok := unmarshalRequest([]byte(`{not json`))
	if ok {
		t.Errorf("expected !ok for malformed JSON, got %+v", req)
	}
}

func TestUnmarshalRequest_WrongFieldType(t *testing.T) {
	// "method" as a number is malformed — we should reject rather than coerce to "".
	data := []byte(`{"jsonrpc":"2.0","id":1,"method":123}`)
	_, ok := unmarshalRequest(data)
	if ok {
		t.Error("expected !ok when method is not a string")
	}
}

func TestHandleRequest_Initialize(t *testing.T) {
	req := JSONRPCRequest{Method: "initialize", ID: json.RawMessage(`1`), hasID: true}
	resp := handleRequest(req, "ws-1", nil)
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize returned error: %+v", resp)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
}

func TestHandleRequest_UnknownMethod(t *testing.T) {
	req := JSONRPCRequest{Method: "frobnicate", ID: json.RawMessage(`1`), hasID: true}
	resp := handleRequest(req, "ws-1", nil)
	if resp.Error == nil {
		t.Fatal("expected error response for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestHandleRequest_ToolsList(t *testing.T) {
	req := JSONRPCRequest{Method: "tools/list", ID: json.RawMessage(`1`), hasID: true}
	resp := handleRequest(req, "ws-1", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list returned error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	got := result["tools"].([]ToolDef)
	if len(got) != len(tools) {
		t.Errorf("got %d tools, want %d", len(got), len(tools))
	}
}
