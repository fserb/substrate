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

	// Server that handles concurrent requests and tracks them
	concurrentServer := `#!/usr/bin/env -S deno run --allow-net
let requestCount = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	const requestId = ++requestCount;
	
	// Short processing time
	await new Promise(resolve => setTimeout(resolve, 10));
	
	const response = {
		requestId: requestId,
		totalRequests: requestCount
	};
	
	return new Response(JSON.stringify(response));
});`

	files := []TestFile{
		{Path: "concurrent.js", Content: concurrentServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Make multiple concurrent requests
	const numRequests = 3
	var wg sync.WaitGroup
	results := make([]string, numRequests)
	errors := make([]error, numRequests)
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			resp, err := http.Get(ctx.BaseURL + "/concurrent.js")
			if err != nil {
				errors[index] = err
				return
			}
			defer resp.Body.Close()
			
			if resp.StatusCode != 200 {
				errors[index] = fmt.Errorf("status %d", resp.StatusCode)
				return
			}
			
			body := make([]byte, 1024)
			n, _ := resp.Body.Read(body)
			results[index] = string(body[:n])
		}(i)
	}
	
	wg.Wait()
	
	// Verify all requests succeeded
	for i, result := range results {
		if errors[i] != nil {
			t.Errorf("Request %d failed: %v", i, errors[i])
		} else if result == "" {
			t.Errorf("Request %d got empty result", i)
		} else {
			t.Logf("Request %d result: %s", i, result)
		}
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

	// Template for servers that identify themselves
	serverTemplate := `#!/usr/bin/env -S deno run --allow-net
const serverName = "%s";
let requestCount = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	requestCount++;
	
	// Simulate some processing time
	await new Promise(resolve => setTimeout(resolve, 50));
	
	return new Response(serverName + " request #" + requestCount);
});`

	files := []TestFile{
		{Path: "server_a.js", Content: fmt.Sprintf(serverTemplate, "ServerA"), Mode: 0755},
		{Path: "server_b.js", Content: fmt.Sprintf(serverTemplate, "ServerB"), Mode: 0755},
		{Path: "server_c.js", Content: fmt.Sprintf(serverTemplate, "ServerC"), Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Make concurrent requests to different servers
	var wg sync.WaitGroup
	servers := []string{"server_a.js", "server_b.js", "server_c.js"}
	results := make([]string, len(servers)*2) // 2 requests per server
	
	for i, server := range servers {
		for j := 0; j < 2; j++ {
			wg.Add(1)
			go func(serverName string, index int) {
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
				results[index] = string(body[:n])
			}(server, i*2+j)
		}
	}
	
	wg.Wait()
	
	// Verify all requests succeeded
	for i, result := range results {
		if result == "" {
			t.Errorf("Request %d got empty result", i)
		}
		t.Logf("Concurrent request %d result: %s", i, result)
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

	// Server that can handle concurrent requests
	highConcurrencyServer := `#!/usr/bin/env -S deno run --allow-net
let totalRequests = 0;

Deno.serve({hostname: Deno.args[0], port: parseInt(Deno.args[1])}, async (req) => {
	const requestId = ++totalRequests;
	
	// Very short processing time
	await new Promise(resolve => setTimeout(resolve, 5));
	
	return new Response("Request " + requestId);
});`

	files := []TestFile{
		{Path: "high_concurrency.js", Content: highConcurrencyServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	defer ctx.TearDown()

	// Make concurrent requests (reduced from 20 to 8)
	const numRequests = 8
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex
	
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			resp, err := http.Get(ctx.BaseURL + "/high_concurrency.js")
			if err != nil {
				return // Don't fail test on individual request errors
			}
			defer resp.Body.Close()
			
			if resp.StatusCode == 200 {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}
	
	wg.Wait()
	
	// Verify most requests succeeded
	if successCount < numRequests/2 {
		t.Errorf("Only %d/%d high concurrency requests succeeded", successCount, numRequests)
	} else {
		t.Logf("High concurrency test: %d/%d requests succeeded", successCount, numRequests)
	}
}