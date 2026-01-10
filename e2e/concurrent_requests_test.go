package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentRequestsToSameProcess(t *testing.T) {
	concurrentServer := `let requestCount = 0;

Deno.serve({path: Deno.args[0]}, async (req) => {
	await new Promise(resolve => setTimeout(resolve, 5));

	return new Response((++requestCount).toString());
});`

	files := []TestFile{
		{Path: "concurrent.js", Content: concurrentServer},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

	const numRequests = 8
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
	serverTemplate := `const serverName = "%s";
let requestCount = 0;

Deno.serve({path: Deno.args[0]}, async (req) => {
	await new Promise(resolve => setTimeout(resolve, 50));

	return new Response(serverName + " request #" + (++requestCount));
});`

	files := []TestFile{
		{Path: "server_a.js", Content: fmt.Sprintf(serverTemplate, "ServerA")},
		{Path: "server_b.js", Content: fmt.Sprintf(serverTemplate, "ServerB")},
		{Path: "server_c.js", Content: fmt.Sprintf(serverTemplate, "ServerC")},
	}

	ctx := RunE2ETest(t, StandardServerBlock(), files)

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
