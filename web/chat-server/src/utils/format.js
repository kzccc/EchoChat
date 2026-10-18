// 展示层格式化工具：时间、文件大小、会话键、头像兜底等。
import { ref } from "vue";

/* ---------------- 时间 ---------------- */

const pad = (n) => (n < 10 ? `0${n}` : `${n}`);

/** 把后端时间字符串（2006-01-02 15:04:05 或 ISO）解析成 Date。 */
export const parseTime = (value) => {
  if (!value) return null;
  if (value instanceof Date) return value;
  const normalized = String(value).replace(" ", "T");
  const date = new Date(normalized);
  return Number.isNaN(date.getTime()) ? null : date;
};

const isSameDay = (a, b) =>
  a.getFullYear() === b.getFullYear() &&
  a.getMonth() === b.getMonth() &&
  a.getDate() === b.getDate();

/** 会话列表用的时间：今天显示时分，昨天显示"昨天"，更早显示日期。 */
export const formatListTime = (value) => {
  const date = parseTime(value);
  if (!date) return "";
  const now = new Date();
  if (isSameDay(date, now)) return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  const yesterday = new Date(now.getTime() - 86400000);
  if (isSameDay(date, yesterday)) return "昨天";
  if (date.getFullYear() === now.getFullYear()) {
    return `${date.getMonth() + 1}月${date.getDate()}日`;
  }
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`;
};

/** 聊天区时间分隔条。 */
export const formatMsgTime = (value) => {
  const date = parseTime(value);
  if (!date) return "";
  const now = new Date();
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (isSameDay(date, now)) return time;
  const yesterday = new Date(now.getTime() - 86400000);
  if (isSameDay(date, yesterday)) return `昨天 ${time}`;
  if (date.getFullYear() === now.getFullYear()) {
    return `${date.getMonth() + 1}月${date.getDate()}日 ${time}`;
  }
  return `${date.getFullYear()}年${date.getMonth() + 1}月${date.getDate()}日 ${time}`;
};

/** 相邻两条消息间隔超过 5 分钟就插入一条时间分隔。 */
export const needTimeDivider = (prev, current) => {
  const a = parseTime(prev?.created_at);
  const b = parseTime(current?.created_at);
  if (!b) return false;
  if (!a) return true;
  return b.getTime() - a.getTime() > 5 * 60 * 1000;
};

/* ---------------- 文本 ---------------- */

export const formatFileSize = (value) => {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (/[a-zA-Z]/.test(raw)) return raw; // 后端已经是 "12.3KB" 这类格式
  const bytes = Number(raw);
  if (!Number.isFinite(bytes) || bytes <= 0) return raw;
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)}MB`;
};

export const isImageFile = (fileName, url) => {
  const target = String(fileName || url || "").toLowerCase();
  return /\.(png|jpe?g|gif|webp|bmp|svg)$/.test(target);
};

/** 头像兜底：取昵称首字，中文取第一个字，英文取首字母。 */
export const avatarText = (name) => {
  const value = String(name || "").trim();
  if (!value) return "?";
  return value.slice(0, 1).toUpperCase();
};

/** 把后端返回的相对路径拼成可访问地址。 */
export const resolveAssetUrl = (backendUrl, path) => {
  if (!path) return "";
  if (/^https?:\/\//i.test(path)) return path;
  if (path.startsWith("/")) return `${backendUrl}${path}`;
  return `${backendUrl}/${path}`;
};

/**
 * 判断是否只是"默认占位头像"。
 *
 * 后端新用户默认头像指向外部图床（cube.elemecdn.com）。这类地址每张要几十到几百毫秒，
 * 列表里十几个人一放就会并发排队，点一下再拉一张，界面就明显发滞。
 * 既然它只是占位图，直接当作"没有头像"，用昵称首字渲染，反而更快也更统一。
 */
const PLACEHOLDER_AVATAR_HOSTS = ["cube.elemecdn.com", "default_avatar.png"];
export const isPlaceholderAvatar = (path) => {
  const value = String(path || "").trim();
  if (!value) return true;
  return PLACEHOLDER_AVATAR_HOSTS.some((host) => value.includes(host));
};

/* ---------------- 会话键 ---------------- */

/** 与后端 model.BuildConversationKey 完全一致的会话键拼接规则。 */
export const buildConversationKey = (oneId, otherId) => {
  if (!oneId || !otherId) return "";
  if (String(otherId).startsWith("G")) return `group:${otherId}`;
  if (String(oneId).startsWith("G")) return `group:${oneId}`;
  return oneId < otherId ? `user:${oneId}:${otherId}` : `user:${otherId}:${oneId}`;
};

/* ---------------- 其它 ---------------- */

/** 生成客户端消息号，用于入口幂等（同一消息重发不会变成两条）。 */
export const newClientMsgId = () => {
  const random = Math.random().toString(36).slice(2, 10);
  return `${Date.now().toString(36)}${random}`;
};

/** 轻量防抖，用于列表刷新之类的场景。 */
export const debounce = (fn, wait = 300) => {
  let timer = null;
  return (...args) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      fn(...args);
    }, wait);
  };
};

/** 判断某个 id 是否是群聊（后端群 id 统一以 G 开头）。 */
export const isGroupId = (id) => String(id || "").startsWith("G");

/** 简单的响应式画布宽度，用于窄屏下的单列切换。 */
export const useNarrowScreen = () => {
  const narrow = ref(window.innerWidth <= 720);
  const update = () => {
    narrow.value = window.innerWidth <= 720;
  };
  window.addEventListener("resize", update);
  return narrow;
};
