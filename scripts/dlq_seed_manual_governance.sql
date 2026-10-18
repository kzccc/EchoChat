-- =============================================================================
-- dlq_message 人工治理测试数据
--
-- 用途：给 /dlq/list、/dlq/detail、/dlq/logs、claim、reopen、close、remark
--       这几个人工治理接口造出各种状态的数据，覆盖:
--         stage          deserialize / route / session_seq / mysql_persist /
--                        websocket_dispatch / group_member_query /
--                        conversation_bucket_worker_panic
--         failure_type    permanent / temporary
--         handle_type     manual / auto
--         status          manual / pending / retrying / done
--         manual_status   open / in_progress / closed
--         close_reason    discarded / externally_fixed / expected /
--                         merged_into_incident
--
-- 说明：
--   1. 显式使用 9001~9014 作为主键，避免和线上真实数据冲突；
--      插入后表的 AUTO_INCREMENT 会被顶到 9015，属正常现象。
--   2. payload_snapshot / context_snapshot 是 JSON 列，必须是合法 JSON 或 NULL；
--      raw_payload 是 longtext NOT NULL，必须是非空字符串。
--   3. (topic, partition_id, offset_id, stage) 上有唯一索引，本脚本每一行都不同。
--   4. last_error 故意写成中文，方便前端列表直接看出是什么故障。
--
-- 先选好库，再整段执行。
-- =============================================================================

-- -----------------------------------------------------------------------------
-- 清理：重复执行前先删掉上一批
-- -----------------------------------------------------------------------------
DELETE FROM dlq_operation_log WHERE dlq_id BETWEEN 9001 AND 9014;
DELETE FROM dlq_message       WHERE id     BETWEEN 9001 AND 9014;

-- =============================================================================
-- 一、主表数据
-- =============================================================================

-- 9001 deserialize：原始报文彻底坏掉，解析阶段就失败，没有业务快照
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9001, 'M9001', 'user:U1001:U1002', 0,
 '{"message_id":"M9001","content":"这条报文的引号没闭合}',
 NULL, NULL,
 'chat_message', 0, 100001, 'deserialize', 'invalid_payload', 'invalid character in string escape code',
 'permanent', 'manual', 'manual', 'open', NULL,
 0, 0, NULL,
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 180 MINUTE), DATE_SUB(NOW(), INTERVAL 180 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 180 MINUTE), DATE_SUB(NOW(), INTERVAL 180 MINUTE));

-- 9002 route：receive_id 为空，无法路由
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9002, 'M9002', '', 0,
 '{"message_id":"M9002","client_msg_id":"C9002","session_id":"S9002","type":0,"content":"缺少收件人","send_id":"U1003","send_name":"小王","receive_id":""}',
 '{"message_id":"M9002","client_msg_id":"C9002","session_id":"S9002","type":0,"content":"缺少收件人","send_id":"U1003","send_name":"小王","receive_id":""}',
 NULL,
 'chat_message', 0, 100002, 'route', 'invalid_route', 'receive_id is empty',
 'permanent', 'manual', 'manual', 'open', NULL,
 0, 0, NULL,
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 165 MINUTE), DATE_SUB(NOW(), INTERVAL 165 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 165 MINUTE), DATE_SUB(NOW(), INTERVAL 165 MINUTE));

-- 9003 route：不支持的消息类型（type=1 Voice 没有对应处理分支），已被人工接手
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9003, 'M9003', 'user:U1003:U1004', 0,
 '{"message_id":"M9003","client_msg_id":"C9003","session_id":"S9003","type":1,"content":"语音消息走错了版本","send_id":"U1003","send_name":"小王","receive_id":"U1004"}',
 '{"message_id":"M9003","client_msg_id":"C9003","session_id":"S9003","type":1,"content":"语音消息走错了版本","send_id":"U1003","send_name":"小王","receive_id":"U1004"}',
 NULL,
 'chat_message', 0, 100003, 'route', 'invalid_route', 'unsupported message type: 1',
 'permanent', 'manual', 'manual', 'in_progress', NULL,
 0, 0, NULL,
 'ops_zhang', DATE_SUB(NOW(), INTERVAL 40 MINUTE), '客户端版本过旧，让学生灰度先关掉语音入口',
 DATE_SUB(NOW(), INTERVAL 150 MINUTE), DATE_SUB(NOW(), INTERVAL 150 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 150 MINUTE), DATE_SUB(NOW(), INTERVAL 40 MINUTE));

-- 9004 session_seq：取号失败（Redis 抖动），自动重投预算还没用完
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9004, 'M9004', 'user:U1005:U1006', 0,
 '{"message_id":"M9004","client_msg_id":"C9004","session_id":"S9004","type":0,"content":"取号时 Redis 超时了","send_id":"U1005","send_name":"小李","receive_id":"U1006"}',
 '{"message_id":"M9004","client_msg_id":"C9004","session_id":"S9004","type":0,"content":"取号时 Redis 超时了","send_id":"U1005","send_name":"小李","receive_id":"U1006"}',
 NULL,
 'chat_message', 1, 100204, 'session_seq', 'session_seq_failed', 'redis: connection pool timeout',
 'temporary', 'auto', 'pending', 'open', NULL,
 2, 5, DATE_ADD(NOW(), INTERVAL 10 SECOND),
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 20 MINUTE), DATE_SUB(NOW(), INTERVAL 3 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 20 MINUTE), DATE_SUB(NOW(), INTERVAL 3 MINUTE));

-- 9005 session_seq：自动重投预算耗尽，已升级到人工
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9005, 'M9005', 'user:U1007:U1008', 0,
 '{"message_id":"M9005","client_msg_id":"C9005","session_id":"S9005","type":0,"content":"自动重投五次仍然失败","send_id":"U1007","send_name":"小赵","receive_id":"U1008"}',
 '{"message_id":"M9005","client_msg_id":"C9005","session_id":"S9005","type":0,"content":"自动重投五次仍然失败","send_id":"U1007","send_name":"小赵","receive_id":"U1008"}',
 NULL,
 'chat_message', 1, 100205, 'session_seq', 'session_seq_failed', 'redis: connection pool timeout',
 'temporary', 'auto', 'manual', 'open', NULL,
 5, 5, NULL,
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 12 MINUTE), DATE_SUB(NOW(), INTERVAL 2 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 12 MINUTE), DATE_SUB(NOW(), INTERVAL 2 MINUTE));

-- 9006 mysql_persist：会话顺序号真冲突，人工判断中
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9006, 'M9006', 'user:U1009:U1010', 1024,
 '{"uuid":"M9006","session_id":"S9006","type":0,"content":"顺序号撞车了","send_id":"U1009","receive_id":"U1010","conversation_key":"user:U1009:U1010","session_seq":1024,"status":0}',
 '{"uuid":"M9006","session_id":"S9006","type":0,"content":"顺序号撞车了","send_id":"U1009","receive_id":"U1010","conversation_key":"user:U1009:U1010","session_seq":1024,"status":0}',
 NULL,
 'chat_message', 2, 100306, 'mysql_persist', 'conversation_seq_conflict', 'Error 1062: Duplicate entry U1009:U1010-1024 for key uniq_message_conversation_seq',
 'permanent', 'manual', 'manual', 'in_progress', NULL,
 0, 0, NULL,
 'ops_zhang', DATE_SUB(NOW(), INTERVAL 25 MINUTE), '疑似同会话多实例同时取号，正在核对两个实例的取号日志',
 DATE_SUB(NOW(), INTERVAL 90 MINUTE), DATE_SUB(NOW(), INTERVAL 90 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 90 MINUTE), DATE_SUB(NOW(), INTERVAL 25 MINUTE));

-- 9007 mysql_persist：消息 ID 重复，已确认是外部原因并关闭
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9007, 'M9007', 'user:U1011:U1012', 1025,
 '{"uuid":"M9007","session_id":"S9007","type":0,"content":"重复落库","send_id":"U1011","receive_id":"U1012","conversation_key":"user:U1011:U1012","session_seq":1025,"status":0}',
 '{"uuid":"M9007","session_id":"S9007","type":0,"content":"重复落库","send_id":"U1011","receive_id":"U1012","conversation_key":"user:U1011:U1012","session_seq":1025,"status":0}',
 NULL,
 'chat_message', 2, 100307, 'mysql_persist', 'uuid_duplicate', 'Error 1062: Duplicate entry M9007 for key uniq_message_uuid',
 'permanent', 'manual', 'manual', 'closed', 'externally_fixed',
 0, 0, NULL,
 'ops_li', DATE_SUB(NOW(), INTERVAL 200 MINUTE), '定位为消费组再均衡导致的重放，库里已有该消息，无需处理',
 DATE_SUB(NOW(), INTERVAL 220 MINUTE), DATE_SUB(NOW(), INTERVAL 220 MINUTE), DATE_SUB(NOW(), INTERVAL 195 MINUTE),
 DATE_SUB(NOW(), INTERVAL 220 MINUTE), DATE_SUB(NOW(), INTERVAL 195 MINUTE));

-- 9008 mysql_persist：数据库死锁，自动重投还没跑完
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9008, 'M9008', 'group:G2001', 2048,
 '{"uuid":"M9008","session_id":"S9008","type":0,"content":"群消息批量落库死锁","send_id":"U1013","receive_id":"G2001","conversation_key":"group:G2001","session_seq":2048,"status":0}',
 '{"uuid":"M9008","session_id":"S9008","type":0,"content":"群消息批量落库死锁","send_id":"U1013","receive_id":"G2001","conversation_key":"group:G2001","session_seq":2048,"status":0}',
 NULL,
 'chat_message', 3, 100408, 'mysql_persist', 'mysql_persist_failed', 'Error 1213: Deadlock found when trying to get lock; try restarting transaction',
 'temporary', 'auto', 'pending', 'open', NULL,
 1, 5, DATE_ADD(NOW(), INTERVAL 10 SECOND),
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 15 MINUTE), DATE_SUB(NOW(), INTERVAL 5 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 15 MINUTE), DATE_SUB(NOW(), INTERVAL 5 MINUTE));

-- 9009 mysql_persist：顺序号冲突，人工确认是脏数据并丢弃
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9009, 'M9009', 'group:G2001', 1030,
 '{"uuid":"M9009","session_id":"S9009","type":0,"content":"灰度实例取号错乱","send_id":"U1014","receive_id":"G2001","conversation_key":"group:G2001","session_seq":1030,"status":0}',
 '{"uuid":"M9009","session_id":"S9009","type":0,"content":"灰度实例取号错乱","send_id":"U1014","receive_id":"G2001","conversation_key":"group:G2001","session_seq":1030,"status":0}',
 NULL,
 'chat_message', 3, 100409, 'mysql_persist', 'conversation_seq_conflict', 'Error 1062: Duplicate entry G2001-1030 for key uniq_message_conversation_seq',
 'permanent', 'manual', 'manual', 'closed', 'discarded',
 0, 0, NULL,
 'ops_zhang', DATE_SUB(NOW(), INTERVAL 300 MINUTE), '已下线灰度实例，这条消息属于错误的取号结果，确认丢弃',
 DATE_SUB(NOW(), INTERVAL 330 MINUTE), DATE_SUB(NOW(), INTERVAL 330 MINUTE), DATE_SUB(NOW(), INTERVAL 295 MINUTE),
 DATE_SUB(NOW(), INTERVAL 330 MINUTE), DATE_SUB(NOW(), INTERVAL 295 MINUTE));

-- 9010 websocket_dispatch：响应体序列化失败，自动重投预算已耗尽
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9010, 'M9010', 'user:U1015:U1016', 3011,
 '{"uuid":"M9010","type":3,"send_id":"U1015","send_name":"小周","receive_id":"U1016","av_data":"{\\"type\\":\\"start_call\\",\\"message_id\\":\\"PROXY\\"}"}',
 '{"uuid":"M9010","type":3,"send_id":"U1015","send_name":"小周","receive_id":"U1016","av_data":"{\\"type\\":\\"start_call\\",\\"message_id\\":\\"PROXY\\"}"}',
 '{"send_avatar":"/static/avatars/u1015.png"}',
 'chat_message', 4, 100510, 'websocket_dispatch', 'websocket_dispatch_failed', 'json: unsupported value: NaN',
 'temporary', 'auto', 'manual', 'open', NULL,
 5, 5, NULL,
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 240 MINUTE), DATE_SUB(NOW(), INTERVAL 180 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 240 MINUTE), DATE_SUB(NOW(), INTERVAL 180 MINUTE));

-- 9011 websocket_dispatch：同类问题，但这次自动重投成功了
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9011, 'M9011', 'user:U1017:U1018', 3012,
 '{"uuid":"M9011","type":0,"content":"重投后推送成功","send_id":"U1017","send_name":"小吴","receive_id":"U1018","session_seq":3012}',
 '{"uuid":"M9011","type":0,"content":"重投后推送成功","send_id":"U1017","send_name":"小吴","receive_id":"U1018","session_seq":3012}',
 '{"send_avatar":"/static/avatars/u1017.png"}',
 'chat_message', 4, 100511, 'websocket_dispatch', 'websocket_dispatch_failed', '',
 'temporary', 'auto', 'done', 'open', NULL,
 1, 5, NULL,
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 200 MINUTE), DATE_SUB(NOW(), INTERVAL 200 MINUTE), DATE_SUB(NOW(), INTERVAL 199 MINUTE),
 DATE_SUB(NOW(), INTERVAL 200 MINUTE), DATE_SUB(NOW(), INTERVAL 199 MINUTE));

-- 9012 group_member_query：查群成员超时，正在被自动重投（retrying）
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9012, 'M9012', 'group:G2002', 0,
 '{"uuid":"M9012","session_id":"S9012","type":0,"content":"万人群消息，查成员超时","send_id":"U1019","receive_id":"G2002"}',
 '{"uuid":"M9012","session_id":"S9012","type":0,"content":"万人群消息，查成员超时","send_id":"U1019","receive_id":"G2002"}',
 NULL,
 'chat_message', 5, 100612, 'group_member_query', 'group_member_query_failed', 'context deadline exceeded while loading group members',
 'temporary', 'auto', 'retrying', 'open', NULL,
 3, 5, DATE_ADD(NOW(), INTERVAL 30 SECOND),
 '', NULL, '',
 DATE_SUB(NOW(), INTERVAL 8 MINUTE), DATE_SUB(NOW(), INTERVAL 1 MINUTE), NULL,
 DATE_SUB(NOW(), INTERVAL 8 MINUTE), DATE_SUB(NOW(), INTERVAL 1 MINUTE));

-- 9013 conversation_bucket_worker_panic：单条消息处理 panic，人工确认是预期内的编码缺陷并关闭
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9013, 'M9013', 'user:U1020:U1021', 4013,
 '{"uuid":"M9013","type":3,"send_id":"U1020","send_name":"小郑","receive_id":"U1021","av_data":"{\\"type\\":\\"weird_event\\",\\"message_id\\":\\"PROXY\\"}"}',
 '{"uuid":"M9013","type":3,"send_id":"U1020","send_name":"小郑","receive_id":"U1021","av_data":"{\\"type\\":\\"weird_event\\",\\"message_id\\":\\"PROXY\\"}"}',
 NULL,
 'chat_message', 5, 100613, 'conversation_bucket_worker_panic', 'worker_panic', 'conversation bucket panic: runtime error: index out of range [3] with length 3',
 'permanent', 'manual', 'manual', 'closed', 'expected',
 0, 0, NULL,
 'ops_wang', DATE_SUB(NOW(), INTERVAL 400 MINUTE), '信令里出现了未定义的 av_type，已确认是旧版本客户端的脏数据，下个版本会兜住',
 DATE_SUB(NOW(), INTERVAL 420 MINUTE), DATE_SUB(NOW(), INTERVAL 420 MINUTE), DATE_SUB(NOW(), INTERVAL 395 MINUTE),
 DATE_SUB(NOW(), INTERVAL 420 MINUTE), DATE_SUB(NOW(), INTERVAL 395 MINUTE));

-- 9014 mysql_persist：并入某个已有事故统一处理
INSERT INTO dlq_message
(id, message_id, conversation_key, session_seq, raw_payload, payload_snapshot, context_snapshot,
 topic, partition_id, offset_id, stage, error_code, last_error,
 failure_type, handle_type, status, manual_status, close_reason,
 attempt_count, max_attempt_count, next_retry_at,
 assignee, claimed_at, remark,
 first_failed_at, last_failed_at, resolved_at, created_at, updated_at)
VALUES
(9014, 'M9014', 'user:U1022:U1023', 5014,
 '{"uuid":"M9014","session_id":"S9014","type":2,"content":"文件消息落库时数据库只读","send_id":"U1022","receive_id":"U1023","file_name":"report.pdf","file_size":"2.1MB","file_type":"pdf"}',
 '{"uuid":"M9014","session_id":"S9014","type":2,"content":"文件消息落库时数据库只读","send_id":"U1022","receive_id":"U1023","file_name":"report.pdf","file_size":"2.1MB","file_type":"pdf"}',
 NULL,
 'chat_message', 1, 100214, 'mysql_persist', 'mysql_persist_failed', 'Error 1290: The MySQL server is running with the --read-only option',
 'temporary', 'auto', 'manual', 'closed', 'merged_into_incident',
 5, 5, NULL,
 'ops_li', DATE_SUB(NOW(), INTERVAL 600 MINUTE), '并入 INC-2026-0714 数据库只读事故，与同批次 37 条一起处理',
 DATE_SUB(NOW(), INTERVAL 700 MINUTE), DATE_SUB(NOW(), INTERVAL 700 MINUTE), DATE_SUB(NOW(), INTERVAL 590 MINUTE),
 DATE_SUB(NOW(), INTERVAL 700 MINUTE), DATE_SUB(NOW(), INTERVAL 590 MINUTE));

-- =============================================================================
-- 二、操作日志（详情页的时间线就靠这张表）
-- =============================================================================

INSERT INTO dlq_operation_log
(dlq_id, action, operator, remark, before_manual_status, after_manual_status, created_at)
VALUES
-- 9001 只有创建
(9001, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 180 MINUTE)),

-- 9002 只有创建
(9002, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 165 MINUTE)),

-- 9003 创建 → 接手 → 备注
(9003, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 150 MINUTE)),
(9003, 'claim',      'ops_zhang',  'claim by ops_zhang',                                         'open',        'in_progress', DATE_SUB(NOW(), INTERVAL 40 MINUTE)),
(9003, 'remark',     'ops_zhang',  '客户端版本过旧，让学生灰度先关掉语音入口',                    'in_progress', 'in_progress', DATE_SUB(NOW(), INTERVAL 38 MINUTE)),

-- 9004 自动重投两次
(9004, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 20 MINUTE)),
(9004, 'auto_retry', 'system',     'auto replay failed, requeue at +10s: redis timeout',          'open',        'open',        DATE_SUB(NOW(), INTERVAL 10 MINUTE)),
(9004, 'auto_retry', 'system',     'auto replay failed, requeue at +10s: redis timeout',          'open',        'open',        DATE_SUB(NOW(), INTERVAL 3 MINUTE)),

-- 9005 自动重投耗尽后转人工
(9005, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 12 MINUTE)),
(9005, 'auto_retry', 'system',     'auto replay exhausted after attempt_count=5: redis timeout',   'open',        'open',        DATE_SUB(NOW(), INTERVAL 2 MINUTE)),

-- 9006 创建 → 接手
(9006, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 90 MINUTE)),
(9006, 'claim',      'ops_zhang',  'claim by ops_zhang',                                         'open',        'in_progress', DATE_SUB(NOW(), INTERVAL 25 MINUTE)),

-- 9007 创建 → 接手 → 关闭
(9007, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 220 MINUTE)),
(9007, 'claim',      'ops_li',     'claim by ops_li',                                            'open',        'in_progress', DATE_SUB(NOW(), INTERVAL 200 MINUTE)),
(9007, 'close',      'ops_li',     'externally_fixed: 再均衡重放，库里已有该消息',                'in_progress', 'closed',      DATE_SUB(NOW(), INTERVAL 195 MINUTE)),

-- 9008 自动重投一次
(9008, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 15 MINUTE)),
(9008, 'auto_retry', 'system',     'auto replay failed, requeue at +10s: deadlock',               'open',        'open',        DATE_SUB(NOW(), INTERVAL 5 MINUTE)),

-- 9009 创建 → 接手 → 关闭（丢弃）
(9009, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 330 MINUTE)),
(9009, 'claim',      'ops_zhang',  'claim by ops_zhang',                                         'open',        'in_progress', DATE_SUB(NOW(), INTERVAL 300 MINUTE)),
(9009, 'close',      'ops_zhang',  'discarded: 灰度实例取号错误，确认丢弃',                        'in_progress', 'closed',      DATE_SUB(NOW(), INTERVAL 295 MINUTE)),

-- 9010 自动重投耗尽
(9010, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 240 MINUTE)),
(9010, 'auto_retry', 'system',     'auto replay exhausted after attempt_count=5: json marshal',   'open',        'open',        DATE_SUB(NOW(), INTERVAL 180 MINUTE)),

-- 9011 自动重投成功
(9011, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 200 MINUTE)),
(9011, 'auto_retry', 'system',     'auto replay success',                                        'open',        'open',        DATE_SUB(NOW(), INTERVAL 199 MINUTE)),
(9011, 'done',       'system',     'auto replay success',                                        'open',        'open',        DATE_SUB(NOW(), INTERVAL 199 MINUTE)),

-- 9012 自动重投三次，正在跑
(9012, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 8 MINUTE)),
(9012, 'auto_retry', 'system',     'auto replay claimed',                                        'open',        'open',        DATE_SUB(NOW(), INTERVAL 6 MINUTE)),
(9012, 'auto_retry', 'system',     'auto replay claimed',                                        'open',        'open',        DATE_SUB(NOW(), INTERVAL 3 MINUTE)),
(9012, 'auto_retry', 'system',     'auto replay claimed',                                        'open',        'open',        DATE_SUB(NOW(), INTERVAL 1 MINUTE)),

-- 9013 创建 → 接手 → 关闭（预期内）
(9013, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 420 MINUTE)),
(9013, 'claim',      'ops_wang',   'claim by ops_wang',                                          'open',        'in_progress', DATE_SUB(NOW(), INTERVAL 400 MINUTE)),
(9013, 'close',      'ops_wang',   'expected: 旧版本客户端脏数据，下个版本兜住',                   'in_progress', 'closed',      DATE_SUB(NOW(), INTERVAL 395 MINUTE)),

-- 9014 创建 → 关闭（并入事故）
(9014, 'create',     'system',     'manual record created',                                      '',            'open',        DATE_SUB(NOW(), INTERVAL 700 MINUTE)),
(9014, 'close',      'ops_li',     'merged_into_incident: INC-2026-0714 数据库只读事故',           'open',        'closed',      DATE_SUB(NOW(), INTERVAL 590 MINUTE));

-- =============================================================================
-- 三、校验：跑完之后照着这几条确认数据是对的
-- =============================================================================

-- 1) 总数应该是 14
-- SELECT COUNT(*) AS total FROM dlq_message WHERE id BETWEEN 9001 AND 9014;

-- 2) 各维度分布，确认覆盖齐全
-- SELECT stage,        COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY stage;
-- SELECT failure_type, COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY failure_type;
-- SELECT handle_type,  COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY handle_type;
-- SELECT status,       COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY status;
-- SELECT manual_status,COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY manual_status;
-- SELECT close_reason, COUNT(*) FROM dlq_message WHERE id BETWEEN 9001 AND 9014 GROUP BY close_reason;

-- 3) 自动重投扫描器应该只捞到 9004 / 9008（pending + auto + 到期）
-- SELECT id, stage, status, handle_type, next_retry_at
--   FROM dlq_message
--  WHERE status = 'pending' AND handle_type = 'auto' AND next_retry_at IS NOT NULL AND next_retry_at <= NOW()
--    AND id BETWEEN 9001 AND 9014;

-- 4) 日志条数
-- SELECT dlq_id, COUNT(*) FROM dlq_operation_log WHERE dlq_id BETWEEN 9001 AND 9014 GROUP BY dlq_id ORDER BY dlq_id;
