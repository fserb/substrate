package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessWorkingDirectory(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	// Test script that outputs its current working directory
	cwdServer := `Deno.serve({path: Deno.args[0]}, (req) => {
  return new Response(Deno.cwd());
});`

	files := []TestFile{
		{Path: "cwd_test.js", Content: cwdServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	expectedDir, _ := filepath.EvalSymlinks(ctx.TempDir)
	ctx.AssertGet("/cwd_test.js", expectedDir)
}

func TestNestedDirectoryWorkingDirectory(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	relativeReadServer := `Deno.serve({path: Deno.args[0]}, async (req) => {
	return new Response(await Deno.readTextFile("./data.txt"));
});`

	dataContent := "Hello from nested directory"

	files := []TestFile{
		{Path: "nested/server.js", Content: relativeReadServer, Mode: 0755},
		{Path: "nested/data.txt", Content: dataContent, Mode: 0644},
	}

	ctx := RunE2ETest(t, serverBlock, files)
	ctx.AssertGet("/nested/server.js", dataContent)
}

func TestWorkingDirectoryWithSymlink(t *testing.T) {
	serverBlock := `@js_files {
		path *.js
		file {path}
	}

	reverse_proxy @js_files {
		transport substrate
		to localhost
	}

	file_server`

	cwdServer := `Deno.serve({path: Deno.args[0]}, (req) => {
	return new Response(Deno.cwd());
});`

	files := []TestFile{
		{Path: "actual_server.js", Content: cwdServer, Mode: 0755},
	}

	ctx := RunE2ETest(t, serverBlock, files)

	symlinkPath := filepath.Join(ctx.TempDir, "/sub/symlink_server.js")
	os.MkdirAll(filepath.Dir(symlinkPath), 0755)
	targetPath := filepath.Join(ctx.TempDir, "actual_server.js")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	expectedDir, _ := filepath.EvalSymlinks(ctx.TempDir)
	ctx.AssertGet("/actual_server.js", expectedDir)
	ctx.AssertGet("/sub/symlink_server.js", expectedDir+"/sub")
}

