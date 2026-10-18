<template>
  <section class="wx-main wx-main--muted">
    <!-- 未选择会话时的占位 -->
    <div v-if="!targetId" class="wx-empty">
      <el-icon class="wx-empty__icon"><ChatDotRound /></el-icon>
      <span>选择一个会话开始聊天</span>
    </div>

    <template v-else>
      <header class="wx-chat-header">
        <div>
          <div class="wx-chat-header__title">
            {{ title || targetId }}
            <span v-if="isGroup" class="wx-chat-header__count">
              （{{ memberCount }}）
            </span>
          </div>
          <div class="wx-chat-header__sub">{{ subtitle }}</div>
        </div>
        <div class="wx-chat-header__actions">
          <el-button v-if="isGroup" link @click="openGroupPanel">群信息</el-button>
          <el-button link @click="reload">刷新</el-button>
        </div>
      </header>

      <div ref="scrollRef" class="wx-chat-scroll">
        <div v-if="hasMore" class="wx-load-more">
          <el-button link size="small" :loading="loadingMore" @click="loadMore">
            加载更早的消息
          </el-button>
        </div>

        <div v-if="!loading && !messages.length" class="wx-empty">
          <el-icon class="wx-empty__icon"><ChatLineRound /></el-icon>
          <span>还没有聊天记录，发条消息试试</span>
        </div>

        <template v-for="item in renderList" :key="item.key">
          <div v-if="item.kind === 'time'" class="wx-msg__divider">
            {{ item.text }}
          </div>
          <div v-else-if="item.kind === 'tip'" class="wx-sys-tip">
            <span>{{ item.text }}</span>
          </div>
          <div
            v-else
            class="wx-msg"
            :class="{ 'wx-msg--self': item.self }"
          >
            <wx-avatar
              :name="item.sendName || item.sendId"
              :src="item.sendAvatar"
              :size="38"
            />
            <div class="wx-msg__main">
              <div v-if="isGroup && !item.self" class="wx-msg__sender">
                {{ item.sendName || item.sendId }}
              </div>

              <!-- 图片消息 -->
              <div
                v-if="item.type === 2 && item.isImage"
                class="wx-bubble wx-bubble--media"
              >
                <img
                  class="wx-bubble__image"
                  :src="item.fileUrl"
                  :alt="item.fileName"
                  @click="previewImage(item.fileUrl)"
                />
              </div>

              <!-- 文件消息 -->
              <div
                v-else-if="item.type === 2"
                class="wx-bubble"
                @click="downloadFile(item)"
              >
                <div class="wx-bubble__file">
                  <el-icon style="font-size: 26px; color: var(--wx-link)">
                    <Document />
                  </el-icon>
                  <div>
                    <div class="wx-bubble__file-name">
                      {{ item.fileName || "未命名文件" }}
                    </div>
                    <div class="wx-bubble__file-size">
                      {{ item.fileSize || "" }}
                    </div>
                  </div>
                </div>
              </div>

              <!-- 音视频信令（当前界面不承载通话） -->
              <div v-else-if="item.type === 3" class="wx-bubble">
                通话消息（当前界面暂不支持接听）
              </div>

              <!-- 文本消息 -->
              <div v-else class="wx-bubble">{{ item.content }}</div>

              <div class="wx-msg__meta">
                <span v-if="item.pending" class="wx-msg__pending">发送中…</span>
                <span v-else-if="item.failed" class="wx-msg__failed">发送失败</span>
              </div>
            </div>
          </div>
        </template>
      </div>

      <footer class="wx-composer">
        <div class="wx-composer__toolbar">
          <span class="wx-composer__tool" title="发送图片" @click="pickImage">
            <el-icon><Picture /></el-icon>
          </span>
          <span class="wx-composer__tool" title="发送文件" @click="pickFile">
            <el-icon><Folder /></el-icon>
          </span>
          <span class="wx-composer__tool" title="表情" @click="insertEmoji">
            <el-icon><Sunny /></el-icon>
          </span>
          <span class="wx-hint">
            {{ socketReady ? "Enter 发送 / Shift+Enter 换行" : "连接已断开，正在重连…" }}
          </span>
        </div>

        <textarea
          ref="inputRef"
          v-model="draft"
          class="wx-composer__input"
          placeholder="输入消息…"
          @keydown.enter.exact.prevent="sendText"
        />

        <div class="wx-composer__footer">
          <span class="wx-hint">{{ draft.length }}/2000</span>
          <el-button
            type="primary"
            size="default"
            :disabled="!draft.trim() || !socketReady || sending"
            :loading="sending"
            @click="sendText"
          >
            发送
          </el-button>
        </div>
      </footer>

      <input
        ref="imageInputRef"
        type="file"
        accept="image/*"
        style="display: none"
        @change="onImagePicked"
      />
      <input
        ref="fileInputRef"
        type="file"
        style="display: none"
        @change="onFilePicked"
      />

      <!-- 群信息抽屉 -->
      <el-drawer v-model="groupPanel" title="群信息" size="320px">
        <div v-if="groupInfo">
          <div class="wx-drawer__row">
            <span class="wx-muted">群名称</span>
            <span>{{ groupInfo.name }}</span>
          </div>
          <div class="wx-drawer__row">
            <span class="wx-muted">群公告</span>
            <span>{{ groupInfo.notice || "暂无" }}</span>
          </div>
          <div class="wx-drawer__row">
            <span class="wx-muted">成员数</span>
            <span>{{ groupInfo.member_cnt }}</span>
          </div>
          <div class="wx-drawer__row">
            <span class="wx-muted">加群方式</span>
            <span>{{ Number(groupInfo.add_mode) === 0 ? "直接加入" : "需要审核" }}</span>
          </div>
        </div>
        <el-divider />
        <div class="wx-drawer__members">
          <div v-for="member in members" :key="member.user_id" class="wx-drawer__member">
            <wx-avatar :name="member.nickname" :src="member.avatar" :size="34" />
            <span class="wx-ellipsis">{{ member.nickname || member.user_id }}</span>
          </div>
          <div v-if="!members.length" class="wx-muted">暂无成员信息</div>
        </div>
        <el-divider />
        <el-button
          v-if="groupInfo && groupInfo.owner_id === meId"
          type="danger"
          plain
          style="width: 100%"
          @click="handleDismissGroup"
        >
          解散该群
        </el-button>
        <el-button
          v-else
          type="danger"
          plain
          style="width: 100%"
          @click="handleLeaveGroup"
        >
          退出该群
        </el-button>
      </el-drawer>
    </template>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage, ElMessageBox } from "element-plus";
import WxAvatar from "@/components/WxAvatar.vue";
import {
  dismissGroup,
  getGroupInfo,
  getGroupMemberList,
  getGroupMessageList,
  getMessageList,
  leaveGroup,
  markRead,
  openSession,
  uploadChatFile,
} from "@/api";
import {
  buildConversationKey,
  debounce,
  formatFileSize,
  formatMsgTime,
  isGroupId,
  isImageFile,
  needTimeDivider,
  newClientMsgId,
  parseTime,
  resolveAssetUrl,
} from "@/utils/format";
import { on as onBus } from "@/utils/wsbus";

const store = useStore();
const route = useRoute();
const router = useRouter();

const meId = computed(() => store.state.userInfo?.uuid || "");
const myName = computed(() => store.state.userInfo?.nickname || "");
const myAvatar = computed(() => store.state.userInfo?.avatar || "");

const targetId = computed(() => String(route.params.id || ""));
const isGroup = computed(() => isGroupId(targetId.value));
const otherUserId = computed(() => (isGroup.value ? "" : targetId.value));
const groupId = computed(() => (isGroup.value ? targetId.value : ""));

const sessionId = ref("");
const title = ref("");
const avatar = ref("");
const subtitle = ref("");
const memberCount = ref(0);
const groupInfo = ref(null);
const members = ref([]);
const groupPanel = ref(false);

const messages = ref([]);
const loading = ref(false);
const loadingMore = ref(false);
const hasMore = ref(false);
const beforeSeq = ref(0);
const draft = ref("");
const sending = ref(false);

const scrollRef = ref(null);
const inputRef = ref(null);
const imageInputRef = ref(null);
const fileInputRef = ref(null);

const socketReady = computed(() => {
  const socket = store.state.socket;
  return !!socket && socket.readyState === WebSocket.OPEN;
});

/* ---------------- 渲染用结构：插入时间分隔与系统提示 ---------------- */

const renderList = computed(() => {
  const list = [];
  let prev = null;
  messages.value.forEach((item) => {
    if (item.isTip) {
      list.push({ kind: "tip", key: item.key, text: item.content });
      prev = null;
      return;
    }
    if (needTimeDivider(prev, item)) {
      list.push({
        kind: "time",
        key: `t-${item.key}`,
        text: formatMsgTime(item.createdAt),
      });
    }
    list.push({ kind: "msg", ...item, key: item.key });
    prev = item;
  });
  return list;
});

/* ---------------- 数据装载 ---------------- */

const toViewModel = (raw) => {
  const fileUrl = raw.url ? resolveAssetUrl(store.state.backendUrl, raw.url) : "";
  const sendId = raw.send_id || "";
  return {
    key: raw.message_id || `local-${raw.session_seq}-${sendId}`,
    messageId: raw.message_id || "",
    sendId,
    sendName: raw.send_name || sendId,
    sendAvatar: raw.send_avatar || "",
    type: Number(raw.type ?? 0),
    content: raw.content || "",
    fileName: raw.file_name || "",
    fileSize: formatFileSize(raw.file_size),
    fileUrl,
    isImage: Number(raw.type) === 2 && isImageFile(raw.file_name, raw.url),
    sessionSeq: Number(raw.session_seq || 0),
    createdAt: raw.created_at || "",
    self: sendId === meId.value,
    pending: false,
    failed: false,
  };
};

const sortMessages = () => {
  messages.value.sort((a, b) => {
    // 本地待确认的气泡始终排在末尾，等服务端回执到了再落到正确位置。
    if (a.pending !== b.pending) return a.pending ? 1 : -1;
    const timeA = parseTime(a.createdAt)?.getTime() || 0;
    const timeB = parseTime(b.createdAt)?.getTime() || 0;
    if (timeA !== timeB) return timeA - timeB;
    return (a.sessionSeq || 0) - (b.sessionSeq || 0);
  });
};

const scrollToBottom = async (smooth = false) => {
  await nextTick();
  const el = scrollRef.value;
  if (!el) return;
  el.scrollTo({ top: el.scrollHeight, behavior: smooth ? "smooth" : "auto" });
};

const loadHistory = async (append = false) => {
  if (!sessionId.value) return;
  if (!append) {
    loading.value = true;
  } else {
    loadingMore.value = true;
  }
  try {
    const before = append ? beforeSeq.value : 0;
    const result = isGroup.value
      ? await getGroupMessageList(groupId.value, before, 30)
      : await getMessageList(meId.value, otherUserId.value, before, 30);

    if (!result.ok) {
      ElMessage.error(result.message || "加载消息失败");
      return;
    }
    const page = result.data || {};
    const rows = Array.isArray(page.list) ? page.list : [];
    const viewRows = rows.map(toViewModel);

    if (append) {
      const known = new Set(messages.value.map((item) => item.key));
      messages.value = [
        ...viewRows.filter((item) => !known.has(item.key)),
        ...messages.value,
      ];
    } else {
      messages.value = viewRows;
    }

    hasMore.value = !!page.has_more;
    beforeSeq.value = Number(page.next_before_seq || 0) || before;

    sortMessages();
    if (!append) {
      await scrollToBottom();
    }
    reportRead();
  } finally {
    loading.value = false;
    loadingMore.value = false;
  }
};

const loadMore = async () => {
  if (!hasMore.value || loadingMore.value) return;
  const el = scrollRef.value;
  const previousHeight = el ? el.scrollHeight : 0;
  await loadHistory(true);
  await nextTick();
  if (el) {
    el.scrollTop = el.scrollHeight - previousHeight;
  }
};

/* ---------------- 已读上报 ---------------- */

const latestSeq = () => {
  let seq = 0;
  messages.value.forEach((item) => {
    if (item.sessionSeq > seq) seq = item.sessionSeq;
  });
  return seq;
};

const reportRead = debounce(async () => {
  const seq = latestSeq();
  if (!seq || !targetId.value) return;
  const key = isGroup.value
    ? buildConversationKey(meId.value, groupId.value)
    : buildConversationKey(meId.value, otherUserId.value);
  if (!key) return;
  await markRead(key, seq);
}, 500);

/* ---------------- 会话身份解析 ---------------- */

const resolveTarget = async () => {
  const query = route.query || {};
  title.value = String(query.name || "");
  avatar.value = String(query.avatar || "");
  sessionId.value = String(query.session_id || "");

  if (isGroup.value) {
    subtitle.value = "群聊";
    await loadGroupMeta();
  } else {
    subtitle.value = "单聊";
  }

  if (!sessionId.value && !isGroup.value) {
    // 从通讯录直接点进来时还没有会话记录，取或建一个。
    const result = await openSession(meId.value, otherUserId.value);
    if (result.ok) {
      sessionId.value = String(result.data || "");
    }
  }

  if (!title.value) {
    title.value = isGroup.value ? groupId.value : otherUserId.value;
  }
};

const loadGroupMeta = async () => {
  const [infoResult, memberResult] = await Promise.all([
    getGroupInfo(groupId.value),
    getGroupMemberList(groupId.value),
  ]);
  if (infoResult.ok && infoResult.data) {
    groupInfo.value = infoResult.data;
    title.value = infoResult.data.name || title.value;
    avatar.value = infoResult.data.avatar || avatar.value;
    memberCount.value = Number(infoResult.data.member_cnt || 0);
  }
  if (memberResult.ok && Array.isArray(memberResult.data)) {
    members.value = memberResult.data;
    if (!memberCount.value) memberCount.value = memberResult.data.length;
  }
};

const reload = async () => {
  messages.value = [];
  beforeSeq.value = 0;
  hasMore.value = false;
  await resolveTarget();
  await loadHistory(false);
};

/* ---------------- 发送 ---------------- */

const ensureSocket = () => {
  if (socketReady.value) return true;
  ElMessage.error("连接已断开，正在重连，请稍后再发");
  return false;
};

const sendPayload = (payload) => {
  const socket = store.state.socket;
  socket.send(JSON.stringify(payload));
};

const basePayload = () => ({
  session_id: sessionId.value,
  send_id: meId.value,
  send_name: myName.value,
  send_avatar: myAvatar.value,
  receive_id: isGroup.value ? groupId.value : otherUserId.value,
  client_msg_id: newClientMsgId(),
});

const appendPending = (partial) => {
  const local = {
    key: `pending-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    messageId: "",
    sendId: meId.value,
    sendName: myName.value,
    sendAvatar: myAvatar.value,
    type: 0,
    content: "",
    fileName: "",
    fileSize: "",
    fileUrl: "",
    isImage: false,
    sessionSeq: 0,
    createdAt: new Date().toISOString(),
    self: true,
    pending: true,
    failed: false,
    ...partial,
  };
  messages.value.push(local);
  scrollToBottom(true);
  return local;
};

const sendText = async () => {
  const content = draft.value.trim();
  if (!content) return;
  if (content.length > 2000) {
    ElMessage.warning("消息太长，最多 2000 字");
    return;
  }
  if (!ensureSocket()) return;
  sending.value = true;
  try {
    sendPayload({ ...basePayload(), type: 0, content });
    draft.value = "";
    appendPending({ type: 0, content });
    inputRef.value?.focus();
  } catch (error) {
    ElMessage.error("发送失败：" + (error?.message || "未知错误"));
  } finally {
    sending.value = false;
  }
};

const sendFileMessage = async (file) => {
  if (!ensureSocket()) return;
  ElMessage.info("正在上传…");
  const result = await uploadChatFile(file);
  if (!result.ok) {
    ElMessage.error(result.message || "上传失败");
    return;
  }
  // 后端按原始文件名落盘，静态目录是 /static/files。
  const url = `/static/files/${file.name}`;
  const isImage = isImageFile(file.name, url);
  sendPayload({
    ...basePayload(),
    type: 2,
    content: "",
    url,
    file_name: file.name,
    file_type: file.type || "",
    file_size: formatFileSize(file.size),
  });
  appendPending({
    type: 2,
    fileName: file.name,
    fileSize: formatFileSize(file.size),
    fileUrl: resolveAssetUrl(store.state.backendUrl, url),
    isImage,
  });
};

const pickImage = () => imageInputRef.value?.click();
const pickFile = () => fileInputRef.value?.click();

const onImagePicked = async (event) => {
  const file = event.target.files?.[0];
  event.target.value = "";
  if (file) await sendFileMessage(file);
};

const onFilePicked = async (event) => {
  const file = event.target.files?.[0];
  event.target.value = "";
  if (file) await sendFileMessage(file);
};

const insertEmoji = () => {
  draft.value += "😊";
  inputRef.value?.focus();
};

const previewImage = (url) => {
  if (url) window.open(url, "_blank");
};

const downloadFile = (item) => {
  if (!item.fileUrl) return;
  const link = document.createElement("a");
  link.href = item.fileUrl;
  link.download = item.fileName || "download";
  link.target = "_blank";
  link.click();
};

/* ---------------- 接收 ---------------- */

const belongsToCurrentChat = (payload) => {
  const sendId = String(payload.send_id || "");
  const receiveId = String(payload.receive_id || "");
  if (isGroup.value) return receiveId === groupId.value;
  return (
    (sendId === otherUserId.value && receiveId === meId.value) ||
    (sendId === meId.value && receiveId === otherUserId.value)
  );
};

/**
 * 服务端会重新分配 message_id，所以本地"发送中"的气泡无法靠 ID 匹配回执。
 * 这里用"类型 + 内容 + 最早的待确认气泡"做一次对账。
 */
const confirmPending = (view) => {
  const index = messages.value.findIndex((item) => {
    if (!item.pending) return false;
    if (item.type !== view.type) return false;
    if (view.type === 2) {
      return item.fileName === view.fileName;
    }
    return item.content === view.content;
  });
  if (index >= 0) {
    messages.value.splice(index, 1);
    return true;
  }
  return false;
};

const handleIncomingMessage = async (payload) => {
  if (!belongsToCurrentChat(payload)) return;
  const view = toViewModel(payload);

  if (view.self) {
    const replaced = confirmPending(view);
    if (replaced) {
      messages.value.push(view);
      sortMessages();
      scrollToBottom(true);
      return;
    }
  }

  if (messages.value.some((item) => item.key === view.key)) return;

  messages.value.push(view);
  sortMessages();
  await scrollToBottom(true);

  if (document.visibilityState === "visible") {
    reportRead();
  }
};

const handleSystemTip = (text) => {
  if (!text) return;
  messages.value.push({
    key: `tip-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
    sendId: "system",
    sendName: "系统",
    sendAvatar: "",
    type: -1,
    content: text,
    sessionSeq: 0,
    createdAt: new Date().toISOString(),
    self: false,
    pending: false,
    failed: false,
    isTip: true,
  });
  scrollToBottom(true);
};

/* ---------------- 群操作 ---------------- */

const openGroupPanel = async () => {
  groupPanel.value = true;
  await loadGroupMeta();
};

const handleLeaveGroup = async () => {
  try {
    await ElMessageBox.confirm("确定退出该群？", "退群", {
      confirmButtonText: "退出",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch (error) {
    return;
  }
  const result = await leaveGroup(meId.value, groupId.value);
  if (!result.ok) {
    ElMessage.error(result.message || "退群失败");
    return;
  }
  ElMessage.success("已退出群聊");
  groupPanel.value = false;
  router.push("/chat/sessionList");
};

const handleDismissGroup = async () => {
  try {
    await ElMessageBox.confirm("解散后所有成员都会失去该群，确定？", "解散群聊", {
      confirmButtonText: "解散",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch (error) {
    return;
  }
  const result = await dismissGroup(meId.value, groupId.value);
  if (!result.ok) {
    ElMessage.error(result.message || "解散失败");
    return;
  }
  ElMessage.success("群聊已解散");
  groupPanel.value = false;
  router.push("/chat/sessionList");
};

/* ---------------- 生命周期 ---------------- */

let offMessage = null;
let offSystem = null;

const bootstrap = async () => {
  if (!targetId.value) return;
  await resolveTarget();
  await loadHistory(false);
  await nextTick();
  inputRef.value?.focus();
};

onMounted(async () => {
  await bootstrap();
  offMessage = onBus("message", handleIncomingMessage);
  offSystem = onBus("system", handleSystemTip);
});

onBeforeUnmount(() => {
  if (offMessage) offMessage();
  if (offSystem) offSystem();
});

watch(
  () => route.params.id,
  async () => {
    if (!targetId.value) {
      messages.value = [];
      return;
    }
    await bootstrap();
  }
);
</script>

<style scoped>
.wx-chat-header__count {
  font-size: 12px;
  color: var(--wx-text-2);
  font-weight: 400;
}
.wx-chat-header__actions {
  display: flex;
  gap: 4px;
}
.wx-load-more {
  text-align: center;
  padding: 4px 0 12px;
}
.wx-msg__meta {
  min-height: 14px;
  font-size: 11px;
  color: var(--wx-text-3);
  padding: 0 2px;
}
.wx-msg__pending {
  color: var(--wx-text-3);
}
.wx-msg__failed {
  color: var(--wx-danger);
}
.wx-bubble--media {
  padding: 4px;
  background: transparent;
  box-shadow: none;
}
.wx-bubble--media .wx-bubble__image {
  border-radius: var(--wx-radius);
}
.wx-drawer__row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  font-size: 13.5px;
  padding: 8px 0;
}
.wx-drawer__members {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 40vh;
  overflow-y: auto;
}
.wx-drawer__member {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13.5px;
}
</style>
