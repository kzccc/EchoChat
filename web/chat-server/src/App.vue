<template>
  <!-- 登录态页面走"左导航 + 列表 + 内容"的微信式外壳；登录/注册页不带外壳。 -->
  <div v-if="isShell" class="wx-app">
    <aside class="wx-rail">
      <div class="wx-rail__avatar" title="我的" @click="go('/chat/owninfo')">
        <wx-avatar :name="nickname" :src="avatar" :size="40" />
      </div>

      <div
        v-for="item in navItems"
        :key="item.path"
        class="wx-rail__item"
        :class="{ 'is-active': isActive(item.path) }"
        :title="item.label"
        @click="go(item.path)"
      >
        <el-icon><component :is="item.icon" /></el-icon>
        <span v-if="item.badge" class="wx-rail__dot">{{ item.badge }}</span>
      </div>

      <div class="wx-rail__spacer" />

      <div
        class="wx-rail__item"
        :class="{ 'is-active': wsState.open }"
        :title="wsState.text"
        @click="toggleSocket"
      >
        <el-icon><component :is="wsState.icon" /></el-icon>
      </div>

      <div class="wx-rail__item" title="退出登录" @click="handleLogout">
        <el-icon><SwitchButton /></el-icon>
      </div>
    </aside>

    <router-view />
  </div>

  <router-view v-else />
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useStore } from "vuex";
import { useRoute, useRouter } from "vue-router";
import axios from "axios";
import { ElMessage } from "element-plus";
import WxAvatar from "@/components/WxAvatar.vue";
import {
  ensureSocketConnected,
  logoutSession,
  normalizeUserInfo,
} from "@/utils/session";
import { emit, parseIncomingFrame } from "@/utils/wsbus";

const store = useStore();
const route = useRoute();
const router = useRouter();

const nickname = computed(() => store.state.userInfo?.nickname || "我");
const avatar = computed(() => store.state.userInfo?.avatar || "");
const isAdmin = computed(() => Number(store.state.userInfo?.is_admin) === 1);

const isShell = computed(() => !!route.meta?.shell);
const isAuthed = computed(
  () =>
    !!store.state.accessToken &&
    !!store.state.refreshToken &&
    !!store.state.userInfo?.uuid
);

const navItems = computed(() => {
  const items = [
    { path: "/chat/sessionList", label: "聊天", icon: "ChatDotRound" },
    { path: "/chat/contactlist", label: "通讯录", icon: "User" },
  ];
  if (isAdmin.value) {
    items.push({ path: "/manager", label: "管理", icon: "Setting" });
  }
  const total = Number(store.state.unreadTotal || 0);
  if (total > 0) {
    items[0].badge = total > 99 ? "99+" : String(total);
  }
  return items;
});

const wsState = ref({ open: false, icon: "Link", text: "连接中" });

const syncWsState = () => {
  const socket = store.state.socket;
  const open = !!socket && socket.readyState === WebSocket.OPEN;
  wsState.value = {
    open,
    icon: open ? "Link" : "Warning",
    text: open ? "已连接" : "未连接（点击重连）",
  };
};

const handleFrame = (raw) => {
  const frame = parseIncomingFrame(raw);
  if (frame.kind === "message") {
    emit("message", frame.payload);
    return;
  }
  if (frame.kind === "system") {
    emit("system", frame.payload);
    return;
  }
  if (frame.kind === "json") {
    emit("json", frame.payload);
  }
};

const connect = () => {
  if (!isAuthed.value) return;
  // 只接管 onMessage：onOpen/onClose/onError 必须留给 session.js 的默认实现，
  // 否则会自动重连（退避调度）失效。
  ensureSocketConnected(store, {
    onMessage: (event) => {
      handleFrame(event.data);
    },
  });
  syncWsState();
};

const toggleSocket = () => {
  if (!isAuthed.value) return;
  const socket = store.state.socket;
  if (socket && socket.readyState === WebSocket.OPEN) {
    ElMessage.success("当前连接正常");
    return;
  }
  connect();
  ElMessage.info("正在重新连接…");
};

const isActive = (path) => {
  if (path === "/chat/sessionList") {
    return route.name === "SessionList" || route.name === "ContactChat";
  }
  return route.path === path;
};

const go = (path) => {
  if (route.path !== path) router.push(path);
};

const refreshUserInfo = async () => {
  const uuid = store.state.userInfo?.uuid;
  if (!uuid) return null;
  try {
    const response = await axios.post(
      store.state.backendUrl + "/user/getUserInfo",
      { uuid }
    );
    if (response?.data?.code === 200) {
      const userInfo = normalizeUserInfo(store, response.data.data);
      store.commit("setUserInfo", userInfo);
      return userInfo;
    }
  } catch (error) {
    console.error(error);
  }
  return null;
};

const handleLogout = async () => {
  await logoutSession(store);
  router.push("/login");
};

let visibilityHandler = null;
let wsStateTimer = null;

onMounted(async () => {
  if (isAuthed.value) {
    const userInfo = await refreshUserInfo();
    if (userInfo && Number(userInfo.status) === 1) {
      await logoutSession(store);
      ElMessage.error("账号已被禁用，请联系管理员。");
      router.push("/login");
      return;
    }
    connect();
  }

  // 连接状态指示灯：session.js 的重连是完全自动的，这里只做轻量轮询刷新指示。
  wsStateTimer = setInterval(syncWsState, 1500);

  // 前后台切换或窗口重新获得焦点时，检查连接健康度，不可用就补连。
  visibilityHandler = () => {
    if (document.visibilityState === "hidden") return;
    if (!isAuthed.value) return;
    const socket = store.state.socket;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      connect();
      return;
    }
    syncWsState();
  };
  document.addEventListener("visibilitychange", visibilityHandler);
  window.addEventListener("focus", visibilityHandler);
});

onBeforeUnmount(() => {
  if (wsStateTimer) clearInterval(wsStateTimer);
  if (visibilityHandler) {
    document.removeEventListener("visibilitychange", visibilityHandler);
    window.removeEventListener("focus", visibilityHandler);
  }
});
</script>
