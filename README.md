# MiniPush

MiniPush is a tiny and simple online push service, as a study project.
Current status: in progress.

For offline push, please refer to:

- [Apple Push Notification service (APNs)](https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/RemoteNotificationsPG/APNSOverview.html#//apple_ref/doc/uid/TP40008194-CH8-SW1)
- [gorush](https://github.com/appleboy/gorush)
- [push-notifications-explained](https://www.airship.com/resources/explainer/push-notifications-explained/)
- [uniqush-push](https://github.com/uniqush/uniqush-push)
- [apns2](https://github.com/sideshow/apns2)

## Overview

Scope:

- features: push from server; optional route bidirectional e2e messages.
- small or medium dataset (at most millions per month).
- low concurrency.

Key decisions:

- event TTL (default: 30 days), outdated events are cleaned by a background task.
- push less, fetch/query more -- events are fetched by clients, server does not push event.
- head sequence as pointer, great for diff.
- compressed read state bitmap.

## Architecture

Components:

- hub: websocket manager.
- gRPC server:
  * route messages: bidirectional stream between clients(hubs) and server.
  * manage cluster sessions.
- store(mysql): events.
- event store manager: consume kafka, clean events.

Data storage:

- mysql (events)
- kafka (incoming events from business servers, optional e2e messages)

Other:

- incoming event (format, size limit)
- create(staging) time
- scalability
- server config

## Event Model And APIs

Every event has the following attributes:

* uid (immutable)
* seq (immutable, sortable)
* body (immutable)
* create_time (immutable, set by callers on event creation)
* read_state (mutable, true/false)

**sequence**

TODO

**event TTL and clean**

TODO

**APIs**

see [proto](proto/api.proto)

## Session Management

**Limit Max Live (online) Sessions**

Rationale:

- avoid flooding by malformed client program, or intentionally abuse by user.
- try avoid interaction with user.

Solution:

- configure max per user session limit (quota): 5?
- kickoff old sessions that beyond the quota.
- delete sessions that was kickoff for a while but hub did not issue delete request.
- delete sessions that no live hub id, caused by follower stop.
- on connect from follower to leader, follower syncs local sessions to leader.

Keep alive:

- websocket Ping/Pong
- Nginx `keepalive_timeout`

## Data Storage

Because the data model is quite simple, we can choose almost any data store.
For small dataset (say millions), `mysql` is enough.
For huge dataset (say billions), perhaps we'd migrate to `tidb` (with sharding).

## WebSocket

[Web socket](https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API) is mature,
but was fully supported since `IE 10 @ win 10`.

Ref: [Scaling to a Millions WebSocket concurrent connections](https://medium.com/@elliekang/scaling-to-a-millions-websocket-concurrent-connections-at-spoon-radio-bbadd6ec1901)

- upgrade
- cors
- nginx
- no load balancing
- no max connection limit on each hub.
