$ nc -z localhost 8722 ; [ $? == 1 ]
$ nc -z localhost 8733 ; [ $? == 1 ]
$ _caddy
*
$ curl -s http://localhost:8722/hello.txt
hello
$ _caddy stop
*

