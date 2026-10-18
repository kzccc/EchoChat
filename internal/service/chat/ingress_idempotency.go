package chat

import (
	"encoding/json"
	"time"

	myredis "echo_chat_server/internal/service/redis"
	"echo_chat_server/pkg/util/random"
	"echo_chat_server/pkg/zlog"

	"go.uber.org/zap"
)

const (
	ingressIdempotencyPendingTTL = 15 * time.Second
	ingressIdempotencyDoneTTL    = 10 * time.Minute
)

type ingressIdempotencySession struct {
	Key         string
	Owner       string
	ClientMsgID string
	SendID      string
	Acquired    bool
}

func beginIngressIdempotency(sendID, clientMsgID string) (ingressIdempotencySession, myredis.IngressIdempotencyRecord, error) {
	session := ingressIdempotencySession{
		Key:         buildIngressIdempotencyKey(sendID, clientMsgID),
		Owner:       "ING" + random.GetNowAndLenRandomString(6),
		ClientMsgID: clientMsgID,
		SendID:      sendID,
	}
	acquired, record, err := myredis.TryAcquireIngressIdempotency(session.Key, session.Owner, ingressIdempotencyPendingTTL)
	if err != nil {
		return ingressIdempotencySession{}, myredis.IngressIdempotencyRecord{}, err
	}
	session.Acquired = acquired
	return session, record, nil
}

func buildIngressIdempotencyKey(sendID, clientMsgID string) string {
	return "ingress_idempotency:" + sendID + ":" + clientMsgID
}

func (s ingressIdempotencySession) completeWithPayload(payload []byte) {
	s.completePayloadForKey(payload)
}

func (s ingressIdempotencySession) completePayloadForKey(payload []byte) {
	if s.Key == "" {
		return
	}
	ok, err := myredis.MarkIngressIdempotencyDone(s.Key, s.Owner, string(payload), ingressIdempotencyDoneTTL)
	if err != nil {
		zlog.Error(
			"message.ingress_idempotency.done_failed",
			zap.String("event", "message.ingress_idempotency.done_failed"),
			zap.String("module", "chat.ingress"),
			zap.String("client_msg_id", s.ClientMsgID),
			zap.String("send_id", s.SendID),
			zap.String("error", err.Error()),
		)
		return
	}
	if !ok {
		zlog.Info(
			"message.ingress_idempotency.done_skipped",
			zap.String("event", "message.ingress_idempotency.done_skipped"),
			zap.String("module", "chat.ingress"),
			zap.String("client_msg_id", s.ClientMsgID),
			zap.String("send_id", s.SendID),
		)
	}
}

func (s ingressIdempotencySession) clearPending() {
	s.clearPendingForKey()
}

func (s ingressIdempotencySession) clearPendingForKey() {
	if s.Key == "" {
		return
	}
	if _, err := myredis.ClearIngressIdempotencyPending(s.Key, s.Owner); err != nil {
		zlog.Error(
			"message.ingress_idempotency.clear_failed",
			zap.String("event", "message.ingress_idempotency.clear_failed"),
			zap.String("module", "chat.ingress"),
			zap.String("client_msg_id", s.ClientMsgID),
			zap.String("send_id", s.SendID),
			zap.String("error", err.Error()),
		)
	}
}

func completeIngressIdempotencyResult(sendID, clientMsgID string, payload []byte) {
	if clientMsgID == "" {
		return
	}
	session := ingressIdempotencySession{
		Key:         buildIngressIdempotencyKey(sendID, clientMsgID),
		ClientMsgID: clientMsgID,
		SendID:      sendID,
	}
	session.completePayloadForKey(payload)
}

func clearIngressIdempotencyPending(sendID, clientMsgID string) {
	if clientMsgID == "" {
		return
	}
	session := ingressIdempotencySession{
		Key:         buildIngressIdempotencyKey(sendID, clientMsgID),
		ClientMsgID: clientMsgID,
		SendID:      sendID,
	}
	session.clearPendingForKey()
}

func replayIngressDoneRecord(client *Client, record myredis.IngressIdempotencyRecord) bool {
	if client == nil || record.Status != myredis.IngressIdempotencyStatusDone || record.Payload == "" {
		return false
	}
	if !json.Valid([]byte(record.Payload)) {
		zlog.Error(
			"message.ingress_idempotency.invalid_payload",
			zap.String("event", "message.ingress_idempotency.invalid_payload"),
			zap.String("module", "chat.ingress"),
		)
		return false
	}
	return client.enqueueBack(&MessageBack{
		Message: []byte(record.Payload),
	})
}

func shouldShortCircuitIngressDuplicate(client *Client, clientMsgID string, record myredis.IngressIdempotencyRecord) bool {
	switch record.Status {
	case myredis.IngressIdempotencyStatusPending:
		zlog.Info(
			"message.ingress_idempotency.pending_duplicate",
			zap.String("event", "message.ingress_idempotency.pending_duplicate"),
			zap.String("module", "chat.ingress"),
			zap.String("client_msg_id", clientMsgID),
		)
		return true
	case myredis.IngressIdempotencyStatusDone:
		zlog.Info(
			"message.ingress_idempotency.done_duplicate",
			zap.String("event", "message.ingress_idempotency.done_duplicate"),
			zap.String("module", "chat.ingress"),
			zap.String("client_msg_id", clientMsgID),
		)
		if replayIngressDoneRecord(client, record) {
			return true
		}
		return true
	default:
		if record.Status != "" {
			zlog.Error(
				"message.ingress_idempotency.unknown_status",
				zap.String("event", "message.ingress_idempotency.unknown_status"),
				zap.String("module", "chat.ingress"),
				zap.String("client_msg_id", clientMsgID),
				zap.String("status", string(record.Status)),
			)
			return true
		}
		return false
	}
}

func mustMarshalIngressReplayPayload(message interface{}) []byte {
	payload, err := json.Marshal(message)
	if err != nil {
		zlog.Error(
			"message.ingress_idempotency.payload_marshal_failed",
			zap.String("event", "message.ingress_idempotency.payload_marshal_failed"),
			zap.String("module", "chat.ingress"),
			zap.String("error", err.Error()),
		)
		return nil
	}
	return payload
}
