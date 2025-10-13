#!/usr/bin/env -S deno run --allow-net --allow-read --allow-write

const [socketPath] = Deno.args;

let cnt = 0;

const server = Deno.serve({
  path: socketPath,
  onListen: () => {},
}, (req) => new Response(`hello ${++cnt}\n`));

