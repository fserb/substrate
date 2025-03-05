$ nc -z localhost 8722 ; [ $? == 1 ]
$ nc -z localhost 8733 ; [ $? == 1 ]
$ _caddy
*
$ curl -s http://localhost:8722/hello.txt
hello
$ curl -s http://localhost:8722/page.md
page
$ curl -w "%{http_code}" -s http://localhost:8722/up
404
$ mv sub substrate
$ curl -s http://localhost:8722/page.md
SUBSTRATE
Request path: http://localhost:8722/page.md
X-Forwarded-Path: /page.md
$ curl -s http://localhost:8722/up
SUBSTRATE
Request path: http://localhost:8722/up
X-Forwarded-Path: /up
$ mv substrate sub
$ curl -s http://localhost:8722/page.md
page
$ curl -w "%{http_code}" -s http://localhost:8722/up
404
$ mv sub substrate
$ curl -s http://localhost:8722/up
SUBSTRATE
Request path: http://localhost:8722/up
X-Forwarded-Path: /up
$ rm -f substrate
$ curl -s http://localhost:8722/page.md
page
$ curl -w "%{http_code}" -s http://localhost:8722/up
404
$ _caddy stop
*

