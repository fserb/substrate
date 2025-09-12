package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentRequestsToSameProcess(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	concurrentServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	await new Promise(resolve => setTimeout(resolve, 10));

	return new Response((++requestCount).toString());
});`

	files := []TestFile{
		{Path: "concurrent.js", Content: concurrentServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	const numRequests = 3
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenNumbers := make(map[string]bool)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			resp, err := http.Get(ctx.BaseURL + "/concurrent.js")
			if err != nil {
				t.Errorf("Request %d failed: %v", index, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				t.Errorf("Request %d got status %d", index, resp.StatusCode)
				return
			}

			body := make([]byte, 1024)
			n, _ := resp.Body.Read(body)
			result := string(body[:n])
			if result == "" {
				t.Errorf("Request %d got empty result", index)
				return
			}

			mu.Lock()
			if seenNumbers[result] {
				t.Errorf("Request %d got duplicate number: %s", index, result)
			} else {
				seenNumbers[result] = true
				t.Logf("Request %d result: %s", index, result)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if len(seenNumbers) != numRequests {
		t.Errorf("Expected %d unique numbers, got %d", numRequests, len(seenNumbers))
	}
}

func TestConcurrentRequestsToDifferentProcesses(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	serverTemplate := `#!/usr/bin/env -S deno run --allow-net
const serverName = "%s";
let requestCount = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	await new Promise(resolve => setTimeout(resolve, 50));

	return new Response(serverName + " request #" + (++requestCount));
});`

	files := []TestFile{
		{Path: "server_a.js", Content: fmt.Sprintf(serverTemplate, "ServerA"), Mode: 0755},
		{Path: "server_b.js", Content: fmt.Sprintf(serverTemplate, "ServerB"), Mode: 0755},
		{Path: "server_c.js", Content: fmt.Sprintf(serverTemplate, "ServerC"), Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	var wg sync.WaitGroup
	var mu sync.Mutex
	servers := []string{"server_a.js", "server_b.js", "server_c.js"}
	seenResponses := make(map[string]bool)

	for i, server := range servers {
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(serverName string, requestIndex int) {
				defer wg.Done()

				resp, err := http.Get(ctx.BaseURL + "/" + serverName)
				if err != nil {
					t.Errorf("Request to %s failed: %v", serverName, err)
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != 200 {
					t.Errorf("Request to %s got status %d", serverName, resp.StatusCode)
					return
				}

				body := make([]byte, 1024)
				n, _ := resp.Body.Read(body)
				result := string(body[:n])
				if result == "" {
					t.Errorf("Request to %s got empty result", serverName)
					return
				}

				mu.Lock()
				if seenResponses[result] {
					t.Errorf("Got duplicate response: %s", result)
				} else {
					seenResponses[result] = true
					t.Logf("Request %d to %s result: %s", requestIndex, serverName, result)
				}
				mu.Unlock()
			}(server, i*2+j)
		}
	}

	wg.Wait()

	if len(seenResponses) != len(servers)*2 {
		t.Errorf("Expected %d unique responses, got %d", len(servers)*2, len(seenResponses))
	}
}

func TestHighConcurrencyToSingleProcess(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}`

	highConcurrencyServer := `#!/usr/bin/env -S deno run --allow-net
let totalRequests = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	await new Promise(resolve => setTimeout(resolve, 5));

	return new Response((++totalRequests).toString());
});`

	files := []TestFile{
		{Path: "high_concurrency.js", Content: highConcurrencyServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	const numRequests = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenNumbers := make(map[string]bool)
	successCount := 0

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			resp, err := http.Get(ctx.BaseURL + "/high_concurrency.js")
			if err != nil {
				t.Logf("Request %d failed: %v", index, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				t.Logf("Request %d got status %d", index, resp.StatusCode)
				return
			}

			body := make([]byte, 1024)
			n, _ := resp.Body.Read(body)
			result := string(body[:n])
			if result == "" {
				t.Logf("Request %d got empty result", index)
				return
			}

			mu.Lock()
			if seenNumbers[result] {
				t.Errorf("Request %d got duplicate number: %s", index, result)
			} else {
				seenNumbers[result] = true
				successCount++
				t.Logf("Request %d result: %s", index, result)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	if successCount < numRequests/2 {
		t.Errorf("Only %d/%d high concurrency requests succeeded with unique numbers", successCount, numRequests)
	} else {
		t.Logf("High concurrency test: %d/%d requests succeeded with unique numbers", successCount, numRequests)
	}

	if len(seenNumbers) != successCount {
		t.Errorf("Expected %d unique numbers, got %d", successCount, len(seenNumbers))
	}
}

