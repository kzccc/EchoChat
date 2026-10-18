// WebSocket 消息总线：连接由 App 统一持有，页面只订阅自己关心的事件。
// 这样避免各页面直接改写 socket.onmessage 而互相覆盖。

const handlers = new Map();

let seq = 0;

/** 订阅一个事件，返回取消订阅函数。 */
export const on = (event, handler) => {
  seq += 1;
  const token = `${event}#${seq}`;
  handlers.set(token, { event, handler });
  return () => handlers.delete(token);
};

/** 派发事件，单个订阅者抛错不影响其它订阅者。 */
export const emit = (event, payload) => {
  handlers.forEach(({ event: name, handler }) => {
    if (name !== event) return;
    try {
      handler(payload);
    } catch (error) {
      console.error("[wsbus] handler failed", error);
    }
  });
};

/* ---------------- 服务端下行消息分类 ---------------- */

/**
 * 后端下发的帧有三种：
 * 1. 消息体（JSON，带 message_id / session_seq）；
 * 2. 系统提示（纯文本，例如"欢迎来到EchoChat聊天服务器"、"已退出登录"）；
 * 3. 关键错误提示（纯文本，例如"消息发送身份校验失败"）。
 * 这里统一解析成 { kind, payload } 交给上层。
 */
export const parseIncomingFrame = (raw) => {
  if (typeof raw !== "string" || raw.trim() === "") {
    return { kind: "unknown", payload: null };
  }
  if (raw.startsWith("{")) {
    try {
      const payload = JSON.parse(raw);
      if (payload && (payload.message_id || payload.av_data)) {
        return { kind: "message", payload };
      }
      return { kind: "json", payload };
    } catch (error) {
      return { kind: "system", payload: raw };
    }
  }
  return { kind: "system", payload: raw };
};
