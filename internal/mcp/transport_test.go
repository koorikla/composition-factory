package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/koorikla/compositionfactory/internal/api"
	"github.com/koorikla/compositionfactory/internal/blueprint"
	"github.com/koorikla/compositionfactory/internal/cache"
	"github.com/koorikla/compositionfactory/internal/index"
	"github.com/koorikla/compositionfactory/internal/schema"
	"github.com/koorikla/compositionfactory/internal/testfixture"
	"github.com/koorikla/compositionfactory/internal/xpkg"
)

func TestResilientTransport_SurvivesMalformedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	crds := testfixture.QueueCRDs(t)
	storeRoot := t.TempDir()
	store := cache.New(storeRoot)
	if err := store.Save(&xpkg.Package{Ref: testProviderRef, Digest: "sha256:test"}, crds); err != nil {
		t.Fatalf("seed provider cache: %v", err)
	}
	idx, err := index.Build(map[string][]schema.CRD{testProviderRef: crds})
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}

	bp := filepath.Join(t.TempDir(), "xqueue.cf.yaml")
	if err := os.WriteFile(bp, []byte(testBlueprintYAML), 0o644); err != nil {
		t.Fatalf("write blueprint: %v", err)
	}
	if _, err := blueprint.Load(bp); err != nil {
		t.Fatalf("test blueprint fixture does not itself validate: %v", err)
	}

	o := api.Options{
		Index:     idx,
		Store:     store,
		Blueprint: bp,
		OutDir:    t.TempDir(),
		Lock:      filepath.Join(t.TempDir(), ".cf.lock"),
		Providers: []string{testProviderRef},
	}

	srv, err := New(o, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Create pipes to simulate client stdin and stdout
	clientInR, clientInW := io.Pipe()   // client writes to clientInW, server reads from clientInR
	serverOutR, serverOutW := io.Pipe() // server writes to serverOutW, client reads from serverOutR

	transport := NewStreamTransport(clientInR, serverOutW)

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- srv.Run(ctx, transport)
	}()

	scanner := bufio.NewScanner(serverOutR)
	readLine := func() string {
		if !scanner.Scan() {
			t.Fatalf("expected output line from server, got EOF / error: %v", scanner.Err())
		}
		return scanner.Text()
	}

	// 1. Initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}` + "\n"
	if _, err := clientInW.Write([]byte(initReq)); err != nil {
		t.Fatalf("write initReq: %v", err)
	}
	initResp := readLine()
	if !strings.Contains(initResp, `"id":1`) {
		t.Fatalf("unexpected init response: %s", initResp)
	}

	// 2. Initialized notification
	initNotif := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	if _, err := clientInW.Write([]byte(initNotif)); err != nil {
		t.Fatalf("write initNotif: %v", err)
	}

	// 3. Malformed non-JSON input
	badLine := "not json at all\n"
	if _, err := clientInW.Write([]byte(badLine)); err != nil {
		t.Fatalf("write badLine: %v", err)
	}
	resp1 := readLine()
	var err1 struct {
		JSONRPC string `json:"jsonrpc"`
		ID      *any   `json:"id"`
		Error   struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp1), &err1); err != nil {
		t.Fatalf("unmarshal resp1: %v (raw: %s)", err, resp1)
	}
	if err1.Error.Code != -32700 {
		t.Errorf("resp1 code = %d, want -32700 (Parse error)", err1.Error.Code)
	}

	// 4. Missing jsonrpc version
	badVersion := `{"id":8,"method":"ping"}` + "\n"
	if _, err := clientInW.Write([]byte(badVersion)); err != nil {
		t.Fatalf("write badVersion: %v", err)
	}
	resp2 := readLine()
	var err2 struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp2), &err2); err != nil {
		t.Fatalf("unmarshal resp2: %v (raw: %s)", err, resp2)
	}
	if err2.Error.Code != -32600 {
		t.Errorf("resp2 code = %d, want -32600 (Invalid Request)", err2.Error.Code)
	}
	if err2.ID != 8 {
		t.Errorf("resp2 ID = %d, want 8", err2.ID)
	}

	// 5. Batch request
	batchReq := `[{"jsonrpc":"2.0","id":8,"method":"ping"},{"jsonrpc":"2.0","id":9,"method":"ping"}]` + "\n"
	if _, err := clientInW.Write([]byte(batchReq)); err != nil {
		t.Fatalf("write batchReq: %v", err)
	}
	resp3 := readLine()
	var err3 struct {
		JSONRPC string `json:"jsonrpc"`
		Error   struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resp3), &err3); err != nil {
		t.Fatalf("unmarshal resp3: %v (raw: %s)", err, resp3)
	}
	if err3.Error.Code != -32600 {
		t.Errorf("resp3 code = %d, want -32600 (batch not supported)", err3.Error.Code)
	}

	// 6. Valid ping AFTER malformed inputs — server must still be alive and responsive!
	pingReq := `{"jsonrpc":"2.0","id":10,"method":"ping"}` + "\n"
	if _, err := clientInW.Write([]byte(pingReq)); err != nil {
		t.Fatalf("write pingReq: %v", err)
	}
	resp4 := readLine()
	if !strings.Contains(resp4, `"id":10`) || strings.Contains(resp4, "error") {
		t.Fatalf("unexpected ping response: %s", resp4)
	}

	// 7. Clean close
	_ = clientInW.Close()

	select {
	case err := <-serverDone:
		if err != nil && err != io.EOF && !strings.Contains(err.Error(), "closed") {
			t.Fatalf("server exited with unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not exit after stdin closed")
	}
}
