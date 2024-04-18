all: build

fmt:
	@gofmt -s -w -d .; \
	goimports --local=github.com/mqy -w -d . ; \
	buildifier -r . ; \
	find . -name *.sh | xargs shfmt -w

proto:
	# protoc, see: https://github.com/protocolbuffers/protobuf/releases
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	protoc \
	--go_out=proto \
	--go_opt=paths=source_relative \
	--go-grpc_out=proto \
	--go-grpc_opt=paths=source_relative \
	proto/api.proto

build: fmt proto
	go build -o dev/bin/minipush .
	go build -o dev/bin/demo-event-producer ./dev/demo 

goreman:
	go get github.com/mattn/goreman

# run three nodes cluster
cluster: build
	cd dev && goreman -f Procfile start

# run one node cluster
cluster-1: build
	cd dev && goreman -f Procfile-1 start

# run standalone node
standalone: build
	cd dev && goreman -f Procfile-standalone start

# requires mockgen v1.6.0+
check-mockgen:
	arr=($$(mockgen --version | sed s/^v// | sed s/\\./\ /g)); \
	if [[ $${arr[0]} -eq 0 || $${arr[1]} -lt 6 ]]; then \
		echo "bad mockgen version, expect v1.6.0+"; \
		exit 1; \
	fi

mockgen: check-mockgen
	mkdir -p store/mock cluster/mock
	mockgen --source store/api.go -package mock IEventStore > store/mock/api.go
	mockgen --source cluster/api.go -package mock IKafka > cluster/mock/api.go

# NOTE: nginx logs dir is also defined in `/dev/nginx.conf`
start-nginx:
	mkdir -p /tmp/nginx-logs
	nginx -c $(PWD)/dev/nginx.conf -e /tmp/nginx-logs/error.log

stop-nginx:
	nginx -c $(PWD)/dev/nginx.conf -e /tmp/nginx-logs/error.log -s stop

lsof-port:
	echo "example usage: PORT=12379 make lsof-port"
	lsof -nP -iTCP -sTCP:LISTEN | grep $(PORT)
	#netstat -anv | grep <port-number>
	#ps -Ao user,pid,command | grep -v grep | grep <PID>

.PHONY: proto