
# Deps

- [kafka_2.13-2.18](https://dlcdn.apache.org/kafka/2.8.0/kafka_2.13-2.8.0.tgz)
- MySQL or MariaDB

## Command line options

```sh
../bin/minipush -h
```

minipush flags:

```
  -addr string
    	server address, ip:port (default "127.0.0.1:8000")
  -demo-static-dir string
    	demo static dir (default "../dev/demo/static")
  -enable-demo
    	enable demo
  -events-enable-clean
    	enable clean(delete) outdated events
  -events-get-limit uint
    	API: getEvents limit (default 25)
  -events-ttl-days uint
    	API: event TTL in days (default 30)
  -kafka-endpoints string
    	comma separated kafka endpoints (default "127.0.0.1:9092")
  -mysql-dsn string
    	mysql server dsn (default "root:root@tcp(127.0.0.1:3306)/minipush?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci")
  -session-quota uint
    	cluster level per user session quota (default 5)
  -standalone
    	run in standalone mode
```

glog flags:

```
  -log_dir string
    	If non-empty, write log files in this directory
  -logtostderr
    	log to standard error instead of files
  -stderrthreshold value
    	logs at or above this threshold go to stderr
  -v value
    	log level for V logs
  -vmodule value
    	comma-separated list of pattern=N settings for file-filtered logging
  -alsologtostderr
    	log to standard error as well as files
```

## Run servers

cd `<dir of push/>`

Run the following commands in each console tab:

```sh
# 1. run mysql/mariadb.

mysqlImage="mysql:8.0.26"
mysqlContainerName="mysql_8.0"
docker run --name "mysql_8.0" --rm -d \
	-e MYSQL_ROOT_PASSWORD=root \
	-e MYSQL_DATABASE=push \
	-p3306:3306 \
	$mysqlImage
sleep 2
mysqlContainerId=$(docker ps | grep "$mysqlContainerName" | awk '{print $1}')
docker cp docs/mysql.sql $mysqlContainerId:/root/
docker exec -it $mysqlContainerId bash
  mysql -u root -p -h 127.0.0.1 < /root/mysql.sql

# enter mysql client:
mysql -u root -p -h 127.0.0.1

# 2. run zookeeper.
cd <path/to>/kafka_2.13-2.8.0/bin
./zookeeper-server-start.sh ../config/zookeeper.properties

# 3. run kafka.
cd <path/to>/kafka_2.13-2.8.0/bin
./kafka-server-start.sh ../config/server.properties

# 4. run minipush server cluster and demo event producer
cd <path/to>/minipush
make cluster-demo

# 5. optional, run nginx as reverse proxy
cd <path/to>/minipush/dev
./nginx.sh

```

## Test in Browser

- open browser tab 1 with http://127.0.0.1:8001/demo show console log
- open browser tab 2 with http://127.0.0.1:8002/demo show console log
- open browser tab 3 with http://127.0.0.1:8003/demo show console log
- open browser tab 1 with http://127.0.0.1/demo show console log

Watch console log changes.

## TODO

- run with docker compose or in k8s.
