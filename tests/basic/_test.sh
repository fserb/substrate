$ nc -z localhost 8722 ; [ $? == 1 ]
$ nc -z localhost 8733 ; [ $? == 1 ]
$ _caddy
*
$ curl -s http://localhost:8722/hello.txt
hello
$ curl -s http://localhost:8722/up
SUBSTRATE
Request path: /up
X-Forwarded-Path: /up
$ curl -s http://localhost:8722/page
SUBSTRATE
Request path: /page
X-Forwarded-Path: /page
$ curl -w "%{http_code}" -s http://localhost:8722/substrate
Not Found
404
$ _caddy stop
*

