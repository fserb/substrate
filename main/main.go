package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	_ "substrate"

	_ "github.com/caddyserver/caddy/v2/modules/standard"
)

func main() {
	caddycmd.Main()
}

