$ nc -z localhost 8722 ; [ $? == 1 ]
$ nc -z localhost 8733 ; [ $? == 1 ]
$ _caddy
*
$ curl -s http://localhost:8722/hello.txt
hello
$ curl -w "%{http_code}" -s http://localhost:8722/up
404
$ curl -s http://localhost:8722/pre/up
SUBSTRATE
Request path: /up
X-Forwarded-Path: /pre/up
UP
$ curl -s http://localhost:8722/page.md
page
$ curl -s http://localhost:8722/pre/page
SUBSTRATE
Request path: /page
X-Forwarded-Path: /pre/page
page
$ curl -w "%{http_code}" -s http://localhost:8722/up
404
$ _caddy stop
*

