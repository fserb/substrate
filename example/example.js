// simplest deno http server

const PORT = 4242;

const sub = Deno.env.get("SUBSTRATE");

console.log("GOING TO", sub);

if (sub) {
  await fetch(sub, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      host: `:${PORT}`,
      match: [".md"],
      paths: ["/up"],
      catch_all: ["/all.md"],
    }),
  });
}

Deno.serve({ port: PORT }, (req) => {
  const path = req.url;
  const fp = req.headers.get("x-forwarded-path");
  return new Response(`Hello, World!

sub: ${sub}
path: ${path}
fp: ${fp}
`);
});

