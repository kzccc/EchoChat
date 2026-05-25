# echochat 在线状态与在线表优化方案

## 目标

这份文档只讨论 `echochat` 当前“在线状态 / 在线表 / 在线路由”这一块，不展开消息主链路、Kafka 批处理、会话摘要或未读数。

目标是回答四件事：

1. 当前在线表到底在系统里承担什么职责
2. 当前实现有哪些明显问题
3. 适合 `echochat` 当前架构的优化方案是什么
4. 哪些事情应该先做，哪些事情属于后续演进

## 当前实现概览

### 1. 进程内在线连接表

当前每个服务实例内部都维护一个内存在线连接表：

- `channel` 模式：`Server.Clients map[string]*Client`
- `kafka` 模式：`KafkaServer.Clients map[string]*Client`

相关代码：

- [internal/service/chat/server.go](/workspace/czk/Personal/KKK/internal/service/chat/server.go:30)
- [internal/service/chat/kafka_server.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_server.go:34)

这个结构的作用是：

- 在当前实例内根据 `client id` 快速找到 websocket 连接
- 本地用户在线时直接 `enqueueBack`
- 避免每次推送都走远程 Redis pub/sub

### 2. Redis 跨实例在线路由表

当前 Kafka 模式下还有一层 Redis 路由表：

- key：`echochat:kafka:ws_route:{clientID}`
- value：`chat-{port}`

相关代码：

- [internal/service/chat/kafka_instance_dispatch.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_instance_dispatch.go:20)
- [internal/service/chat/kafka_instance_dispatch.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_instance_dispatch.go:43)

作用是：

- 某个实例找不到本地连接时，先查这个用户在哪个实例
- 如果在别的实例，就向那个实例对应的 Redis pub/sub channel 发消息

### 3. 当前在线状态的真实语义

严格来说，当前这套东西更像：

- “在线消息投递路由”

而不是：

- “完整用户在线状态系统”

因为它主要回答的是：

- “这个用户当前应该往哪个实例投递”

而不是：

- “这个用户现在是否真的在线”
- “这个用户最后活跃时间是什么”
- “这个用户有几个终端在线”

## 当前实现的主要问题

## 问题一：Redis 路由 key 没有 TTL，异常退出后容易留下脏路由

当前注册路由时调用的是：

- `myredis.SetKey(kafkaWSRouteKey(client.Uuid), k.instanceID)`

相关代码：

- [internal/service/chat/kafka_instance_dispatch.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_instance_dispatch.go:47)
- [internal/service/redis/redis_service.go](/workspace/czk/Personal/KKK/internal/service/redis/redis_service.go:71)

`SetKey` 是永久 key，不过期。

这意味着如果出现下面这些情况：

- 进程 crash
- 容器被 kill
- 节点突然重启
- 连接关闭流程没有正常执行

那么 Redis 里这个用户的实例路由可能会永久残留。

表现出来的问题是：

- 系统误以为该用户还在线
- 远端实例仍然会尝试向这个实例投递
- 需要依赖“投递失败后再清理”这种被动修复

这会降低在线状态的正确性。

## 问题二：在线状态过度依赖正常 Logout/RemoveClient 清理

当前在线路由的清理主要依赖：

- `Logout`
- `RemoveClient`
- `unregisterClientRoute`

相关代码：

- [internal/service/chat/kafka_server.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_server.go:147)
- [internal/service/chat/kafka_server.go](/workspace/czk/Personal/KKK/internal/service/chat/kafka_server.go:743)

这类方案在“正常关闭连接”的情况下没问题，但网络环境里经常出现：

- 客户端断网
- 移动网络切换
- 半开连接
- 节点异常中断

这些情况下不一定会触发完整的清理链路。

所以当前方案的本质问题是：

- 在线态缺少“超时自动失效”的能力

## 问题三：当前在线表只解决“投递去哪”，不解决“用户是否真实在线”

现在 Redis 路由表只记录：

- `client_id -> instance_id`

但不记录：

- `active_at`
- `last_seen`
- `connected_at`
- `device_id`
- `platform`

这会带来几个限制：

1. 无法区分“逻辑上可路由”和“连接真实健康”
2. 无法准确表达“最近活跃时间”
3. 无法给产品层提供“在线 / 刚刚在线 / xx 分钟前在线”
4. 无法为多端在线管理提供信息基础

所以当前在线状态是“路由导向”，不是“状态导向”。

## 问题四：当前模型不适合多端在线和设备粒度管理

当前主要围绕 `client id` 工作，但没有明显的：

- 用户下多个连接集合
- 连接所属设备信息
- 设备级下线
- 同账号多端并发策略

如果后续要支持：

- Web + iOS + Android 同时在线
- 指定设备踢下线
- 指定设备消息同步
- 多端在线状态展示

当前模型会逐渐吃力。

## 问题五：在线状态和产品能力之间没有统一抽象

你现在有：

- 进程内 `Clients map`
- Redis 路由 key

这足够支撑“发消息找连接”，但不够支撑：

- 在线状态查询 API
- 后台在线监控
- 用户最近活跃展示
- 多端会话管理

换句话说：

- 当前在线表服务的是“基础设施”
- 不是“产品层在线能力”

## 当前方案的优点

在谈优化前，也要明确当前方案并不是一无是处。

### 1. 简单

结构很直接：

- 本地先查内存连接
- 查不到再查 Redis 路由
- 最后远程 pub/sub 转发

这套结构易理解，代码路径短。

### 2. 本地命中成本低

本地在线用户的消息分发是非常快的：

- 一个 map 查找
- 一个 channel enqueue

对单实例或小规模多实例来说，效率很好。

### 3. 已经具备跨实例路由能力

这一点很重要。

你已经解决了：

- 用户不在本机时，如何找到目标实例

这说明现在线路不是推倒重来，而是“补齐在线状态正确性和能力层”。

## 最适合当前 echochat 的优化方向

## 方向一：给 Redis 路由表加 TTL

这是最小改动、收益最大的第一步。

### 方案

把当前：

- `SetKey(routeKey, instanceID)`

改成：

- `SetKeyEx(routeKey, routePayload, ttl)`

推荐 TTL：

- `60s` 到 `120s`

理由：

- 太短会导致续租太频繁
- 太长会让脏路由残留时间过久

### 效果

即使实例异常退出，只要心跳停止续租，路由也会在 TTL 后自动失效。

这能显著改善：

- 假在线
- 脏路由
- 异常中断后的错误投递

## 方向二：引入心跳续租，而不是只在登录时写一次

当前路由注册更像“一次登记”，应改成“持续续租”。

### 方案

每个在线连接在以下时机续租：

1. 连接建立成功时
2. 收到客户端心跳时
3. 收到客户端任意业务消息时
4. 可选：服务端主动 ping/pong 成功时

每次续租都刷新：

- TTL
- `active_at`

### 效果

这样在线状态会从：

- “只要没删掉就算在线”

变成：

- “最近一段时间持续活跃才算在线”

## 方向三：把 Redis 路由值从纯字符串升级成结构化在线记录

当前 value 只有：

- `instance_id`

建议升级为 JSON 或 Hash，至少包含：

- `instance_id`
- `connection_id`
- `active_at`
- `connected_at`
- `route_version`

如果后续要支持多端，再扩：

- `device_id`
- `platform`
- `client_type`

### 一个建议结构

```json
{
  "instance_id": "chat-18082",
  "connection_id": "WS123456",
  "active_at": "2026-05-07T10:00:00Z",
  "connected_at": "2026-05-07T09:58:10Z"
}
```

### 效果

好处是：

- 路由层和状态层开始合并
- 后续能更平滑支持最近活跃、在线查询、后台监控

## 方向四：区分“在线路由”与“用户在线状态”

这一点很关键。

建议把两个概念拆开：

### A. 路由层

解决：

- 这个消息该往哪个实例推

### B. 状态层

解决：

- 这个用户是否真的在线
- 最近活跃时间是什么
- 是否多端在线

当前你这两层混在一起了，所以很多时候“路由还在”就被等价成“在线”。

更合理的做法是：

- 路由表服务于消息投递
- 在线状态表服务于产品语义和后台语义

## 方向五：把单连接模型扩成“用户 -> 多连接集合”

如果后续要支持多端，这是迟早要做的。

### 建议结构

按用户维护一个连接集合，例如：

- `echochat:online:user:{userID}` -> set/hash

每个成员包含：

- `instance_id`
- `connection_id`
- `device_id`
- `platform`
- `active_at`

### 效果

这会让你后面更容易做：

- 多端同时在线
- 多端同步消息
- 设备级踢人
- 精确在线人数统计

### 当前是否必须马上做

不是。

如果你现在最关心的是“正确性”和“稳定性”，先做 TTL + 心跳续租就够了。

多端集合模型可以放在第二阶段。

## 方向六：增加实例启动/关闭时的自清理与自恢复策略

即使有 TTL，也建议在实例生命周期里补两件事：

### 启动时

- 扫描并清理属于本实例的历史脏路由
- 或记录一个新的 `instance epoch`

### 关闭时

- 尽量主动删除当前实例所有在线路由

这样可以减少：

- 节点重启后“旧实例 ID 残留”
- 灰度发布或频繁重启时的脏数据影响

## 一个适合当前 echochat 的最小增量方案

如果按“收益 / 改动比”排序，我建议分两步做。

### 第一步：先做正确性修复

只改在线路由，不动消息主链路。

包含：

1. `ws_route` 从永久 key 改为带 TTL
2. 收消息 / 收心跳时续租
3. Redis value 增加 `active_at`
4. 保留现有本地 `Clients map` 不变

这一阶段的目标是：

- 解决假在线
- 解决脏路由
- 提升跨实例投递正确性

### 第二步：再做能力增强

在第一步稳定后再补：

1. `user -> 多连接集合`
2. `device/platform` 维度
3. 在线状态查询接口
4. 最近活跃语义

这一阶段的目标是：

- 支撑多端
- 支撑产品层在线展示
- 支撑后台管理

## 不建议现在做的事

### 1. 不建议推翻本地 `Clients map`

本地 map 查找非常快，而且是 websocket 推送的性能基础。

应该保留：

- 内存 map 做本地投递
- Redis 做跨实例路由和在线状态补充

### 2. 不建议把在线状态查询完全建立在扫描 Redis key 上

你当前 Redis service 里还有 `Keys(prefix*)` 这种能力：

- [internal/service/redis/redis_service.go](/workspace/czk/Personal/KKK/internal/service/redis/redis_service.go:183)

这类方式在 keyspace 大了以后不适合作为正式在线状态查询方案。

### 3. 不建议为了在线状态改动消息主链路

在线状态问题属于“连接层 / 路由层”，不应该牵连：

- Kafka 消费
- MySQL 批量落库
- conversation bucket

先把边界守住，改造成本会低很多。

## 推荐实施顺序

### P0：必须优先做

1. Redis 路由 key 加 TTL
2. 心跳续租
3. value 加 `active_at`

### P1：建议第二批做

1. 区分路由表和状态表语义
2. 增加实例启动/关闭自清理
3. 增加最近活跃时间读取能力

### P2：作为演进方向

1. 用户多连接集合
2. 设备粒度管理
3. 多端在线能力

## 末尾压缩版：每点一句话

1. 当前 Redis 在线路由没有 TTL，容易留下脏在线状态；给 `ws_route` 加过期时间并用心跳持续续租。
2. 当前在线状态过度依赖正常 Logout 清理，异常断连时修正滞后；改成“上线注册 + 心跳续租 + 超时自动失效”。
3. 当前在线表只表达“往哪投递”，不表达“是否真实在线”；在路由记录里补 `active_at/connected_at`。
4. 当前模型不适合多端在线和设备管理；把单路由逐步扩成“用户 -> 多连接集合”。
5. 当前在线表更服务基础设施而非产品能力；把“消息路由”和“在线状态展示”拆成两层语义。
6. 当前实例异常退出后依赖被动修复清理 stale route；补实例启动自清理和关闭时主动摘路由。
7. 当前本地 `Clients map` 其实是优势不是问题；保留内存直达投递，只增强 Redis 层的正确性和表达力。
