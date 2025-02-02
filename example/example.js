// simplest deno http server

const PORT = 4242;

const sub = Deno.env.get("SUBSTRATE");

if (sub) {
  await fetch(sub, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      host: `:${PORT}`,
      try_files: ["/index.md", ".md"],
      match: [".md"],
      catch_all: ["all.md"],
    }),
  });
}

Deno.serve({ port: PORT }, (req) => {
  const path = req.url;
  const cwd = Deno.cwd();
  return new Response("Hello, World! " + sub + " ==> " + path + " at " + cwd);
});

