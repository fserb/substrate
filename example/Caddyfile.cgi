{
	admin localhost:2020
  log {
    format console
    level DEBUG
    }
}

:4323 {
	root example

	@js_files path *.js
	cgi @js_files ./example/{path}
	file_server
}

