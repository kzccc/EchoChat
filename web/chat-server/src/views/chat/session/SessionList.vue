<template>
  <section class="wx-list-pane">
    <div class="wx-pane__search">
      <el-input v-model="keyword" placeholder="搜索" size="default" clearable>
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
    </div>

    <div class="wx-pane__title">
      <span>聊天</span>
      <el-button link size="small" :loading="loading" @click="loadAll">
        刷新
      </el-button>
    </div>

    <div class="wx-pane__scroll">
      <div v-if="!filteredList.length" class="wx-empty">
        <el-icon class="wx-empty__icon"><ChatDotRound /></el-icon>
        <span>{{ keyword ? "没有匹配的会话" : "还没有会话，去通讯录找人聊聊" }}</span>
      </div>

      <div
        v-for="item in filteredList"
        :key="item.key"
        class="wx-row"
        :class="{ 'is-active': item.id === activeId }"
        @click="openChat(item)"
      >
        <wx-avatar :name="item.name" :src="item.avatar" :size="42" />
        <div class="wx-row__body">
          <div class="wx-row__top">
            <span class="wx-row__name">
              <el-icon v-if="item.kind === 'group'" class="wx-row__tag"><UserFilled /></el-icon>
              {{ item.name }}
            </span>
            <span class="wx-row__time">{{ item.timeText }}</span>
          </div>
          <div class="wx-row__sub">
            <span class="wx-ellipsis">{{ item.preview }}</span>
            <span v-if="item.unread > 0" class="wx-badge">
              {{ item.unread > 99 ? "99+" : item.unread }}
            </span>
          </div>
        </div>
        <el-icon class="wx-row__more" @click.stop="removeSession(item)">
          <Delete />
        </el-icon>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage, ElMessageBox } from "element-plus";
import WxAvatar from "@/components/WxAvatar.vue";
import {
  deleteSession,
  getGroupSessionList,
  getUserSessionList,
} from "@/api";
import { debounce } from "@/utils/format";
import { on as onBus } from "@/utils/wsbus";

const store = useStore();
const route = useRoute();
const router = useRouter();

const keyword = ref("");
const loading = ref(false);
const sessions = ref([]);
const activeId = ref("");

const meId = computed(() => store.state.userInfo?.uuid || "");

const filteredList = computed(() => {
  const key = keyword.value.trim().toLowerCase();
  if (!key) return sessions.value;
  return sessions.value.filter((item) =>
    String(item.name || "").toLowerCase().includes(key)
  );
});

const toRow = (raw) => {
  if (raw.kind === "group") {
    return {
      key: `group:${raw.id}`,
      kind: "group",
      id: raw.id,
      sessionId: raw.sessionId,
      name: raw.name || raw.id,
      avatar: raw.avatar || "",
      unread: Number(raw.unread || 0),
      timeText: "",
      preview: "群聊",
    };
  }
  return {
    key: `user:${raw.id}`,
    kind: "user",
    id: raw.id,
    sessionId: raw.sessionId,
    name: raw.name || raw.id,
    avatar: raw.avatar || "",
    unread: Number(raw.unread || 0),
    timeText: "",
    preview: "",
  };
};

const loadAll = async () => {
  if (!meId.value) return;
  loading.value = true;
  try {
    const [userResult, groupResult] = await Promise.all([
      getUserSessionList(meId.value),
      getGroupSessionList(meId.value),
    ]);

    const rows = [];

    if (userResult.ok && Array.isArray(userResult.data)) {
      userResult.data.forEach((item) => {
        rows.push(
          toRow({
            kind: "user",
            id: item.user_id,
            sessionId: item.session_id,
            name: item.user_name,
            avatar: item.avatar,
            unread: item.unread_count,
          })
        );
      });
    }

    if (groupResult.ok && Array.isArray(groupResult.data)) {
      groupResult.data.forEach((item) => {
        rows.push(
          toRow({
            kind: "group",
            id: item.group_id,
            sessionId: item.session_id,
            name: item.group_name,
            avatar: item.avatar,
            unread: item.unread_count,
          })
        );
      });
    }

    // 有未读的排前面，其余按名称稳定排序，避免每次刷新顺序乱跳。
    rows.sort((a, b) => {
      if (a.unread !== b.unread) return b.unread - a.unread;
      return String(a.name).localeCompare(String(b.name), "zh-Hans-CN");
    });

    sessions.value = rows;

    const total = rows.reduce((sum, item) => sum + (item.unread || 0), 0);
    store.commit("setUnreadTotal", total);
  } finally {
    loading.value = false;
  }
};

const openChat = (item) => {
  activeId.value = item.id;
  router.push({
    name: "ContactChat",
    params: { id: item.id },
    query: {
      session_id: item.sessionId || "",
      name: item.name || "",
      avatar: item.avatar || "",
      kind: item.kind,
    },
  });
};

const removeSession = async (item) => {
  try {
    await ElMessageBox.confirm(
      `删除与「${item.name}」的会话？聊天记录不会被删除。`,
      "删除会话",
      { confirmButtonText: "删除", cancelButtonText: "取消", type: "warning" }
    );
  } catch (error) {
    return;
  }
  const result = await deleteSession(meId.value, item.sessionId);
  if (!result.ok) {
    ElMessage.error(result.message || "删除失败");
    return;
  }
  ElMessage.success("已删除");
  await loadAll();
};

const refreshDebounced = debounce(() => {
  loadAll();
}, 400);

let offMessage = null;
let offSystem = null;

onMounted(async () => {
  activeId.value = String(route.params.id || "");
  await loadAll();
  // 收到新消息或系统提示时刷新列表，让未读角标跟上。
  offMessage = onBus("message", refreshDebounced);
  offSystem = onBus("system", refreshDebounced);
});

onBeforeUnmount(() => {
  if (offMessage) offMessage();
  if (offSystem) offSystem();
});
</script>

<style scoped>
.wx-row__sub {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.wx-row__tag {
  font-size: 12px;
  color: var(--wx-text-3);
  vertical-align: -1px;
}
.wx-row__more {
  flex: 0 0 auto;
  color: var(--wx-text-3);
  font-size: 15px;
  opacity: 0;
  transition: opacity 0.15s, color 0.15s;
}
.wx-row:hover .wx-row__more {
  opacity: 1;
}
.wx-row__more:hover {
  color: var(--wx-danger);
}
</style>
