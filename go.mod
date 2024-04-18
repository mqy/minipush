module github.com/mqy/minipush

go 1.19

require (
	github.com/go-sql-driver/mysql v1.6.0
	github.com/gogo/protobuf v1.3.2
	github.com/golang/glog v1.0.0
	github.com/golang/mock v1.6.0
	github.com/gorilla/websocket v1.4.2
	github.com/pborman/uuid v1.2.1
	github.com/prometheus/client_golang v1.11.0
	github.com/segmentio/kafka-go v0.4.27
	github.com/stretchr/testify v1.7.0
	go.etcd.io/bbolt v1.3.6
	golang.org/x/net v0.0.0-20211109214657-ef0fda0de508
	google.golang.org/grpc v1.42.0
	google.golang.org/protobuf v1.27.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/golang/snappy v0.0.1 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/klauspost/compress v1.9.8 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/pierrec/lz4 v2.6.0+incompatible // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.26.0 // indirect
	github.com/prometheus/procfs v0.6.0 // indirect
	golang.org/x/sys v0.0.0-20210603081109-ebe580a85c40 // indirect
	golang.org/x/text v0.3.6 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace (
	github.com/mqy/minipush/auth => ./auth
	github.com/mqy/minipush/cluster => ./cluster
	github.com/mqy/minipush/proto => ./proto
	github.com/mqy/minipush/store => ./store
	github.com/mqy/minipush/t => ./t
	github.com/mqy/minipush/ws => ./ws
)
