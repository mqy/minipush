# Kafka commands

```shell
# List Topics
./kafka-topics.sh --bootstrap-server localhost:9092 --list

# Create Topic
./kafka-topics.sh --bootstrap-server localhost:9092 --create --topic minipush-events --partitions 1 --replication-factor 1

# Delete Topic
./kafka-topics.sh --bootstrap-server localhost:9092 --delete --topic minipush-events
```