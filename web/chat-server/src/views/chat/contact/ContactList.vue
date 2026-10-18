<template>
  <section class="wx-list-pane">
    <div class="wx-pane__search">
      <el-input v-model="keyword" placeholder="搜索联系人" size="default" clearable>
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
    </div>

    <div class="wx-pane__title">
      <span>通讯录</span>
      <el-dropdown trigger="click" @command="onCommand">
        <el-button link size="small">
          新建<el-icon><ArrowDown /></el-icon>
        </el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="friend">添加联系人</el-dropdown-item>
            <el-dropdown-item command="group">新建群聊</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>

    <div class="wx-pane__scroll">
      <!-- 新的朋友 -->
      <div class="wx-row wx-row--entry" @click="toggleSection('requests')">
        <div class="wx-entry__icon" style="background: #f5a623">
          <el-icon><UserFilled /></el-icon>
        </div>
        <div class="wx-row__body">
          <div class="wx-row__top">
            <span class="wx-row__name">新的朋友</span>
            <span v-if="requests.length" class="wx-badge">{{ requests.length }}</span>
          </div>
          <div class="wx-row__sub"><span class="wx-ellipsis">好友申请</span></div>
        </div>
      </div>

      <div v-if="sections.requests" class="wx-request-list">
        <div v-if="!requests.length" class="wx-request-empty">暂无新的申请</div>
        <div v-for="item in requests" :key="item.contact_id" class="wx-request">
          <wx-avatar :name="item.contact_name" :src="item.contact_avatar" :size="34" />
          <div class="wx-request__body">
            <div class="wx-ellipsis">{{ item.contact_name || item.contact_id }}</div>
            <div class="wx-muted wx-ellipsis">{{ item.message || "请求添加你为好友" }}</div>
          </div>
          <el-button size="small" type="primary" @click="acceptRequest(item)">通过</el-button>
          <el-button size="small" @click="refuseRequest(item)">忽略</el-button>
        </div>
      </div>

      <!-- 群聊 -->
      <div class="wx-section-title">群聊</div>
      <div v-if="!groups.length" class="wx-section-empty">还没有加入任何群聊</div>
      <div
        v-for="item in groups"
        :key="item.group_id"
        class="wx-row"
        :class="{ 'is-active': selectedKey === `group:${item.group_id}` }"
        @click="selectGroup(item)"
      >
        <wx-avatar :name="item.group_name" :src="item.avatar" :size="38" />
        <div class="wx-row__body">
          <div class="wx-row__name wx-ellipsis">{{ item.group_name }}</div>
        </div>
      </div>

      <!-- 联系人 -->
      <div class="wx-section-title">联系人（{{ contacts.length }}）</div>
      <div v-if="!contacts.length" class="wx-section-empty">还没有联系人</div>
      <div
        v-for="item in filteredContacts"
        :key="item.id"
        class="wx-row"
        :class="{ 'is-active': selectedKey === `user:${item.id}` }"
        @click="selectContact(item)"
      >
        <wx-avatar :name="item.name" :src="item.avatar" :size="38" />
        <div class="wx-row__body">
          <div class="wx-row__name wx-ellipsis">{{ item.name || item.id }}</div>
          <div class="wx-row__sub">
            <span class="wx-ellipsis">{{ item.id }}</span>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- 右侧资料区 -->
  <section class="wx-main wx-main--muted">
    <div v-if="!selected" class="wx-empty">
      <el-icon class="wx-empty__icon"><User /></el-icon>
      <span>选择一个联系人查看资料</span>
    </div>

    <div v-else class="wx-page">
      <div class="wx-profile">
        <wx-avatar
          :name="selectedName"
          :src="selectedAvatar"
          :size="64"
        />
        <div>
          <div class="wx-profile__name">{{ selectedName }}</div>
          <div class="wx-profile__meta">
            {{ selectedKind === "group" ? "群聊" : "联系人" }}
            <span v-if="selectedId"> · {{ selectedId }}</span>
          </div>
        </div>
      </div>

      <div class="wx-card">
        <div class="wx-card__title">资料</div>
        <div class="wx-field">
          <span class="wx-field__label">昵称</span>
          <span class="wx-field__value">{{ selectedName }}</span>
        </div>
        <div v-if="selectedKind === 'user'" class="wx-field">
          <span class="wx-field__label">手机号</span>
          <span class="wx-field__value">{{ profile.contact_phone || "未填写" }}</span>
        </div>
        <div v-if="selectedKind === 'user'" class="wx-field">
          <span class="wx-field__label">邮箱</span>
          <span class="wx-field__value">{{ profile.contact_email || "未填写" }}</span>
        </div>
        <div v-if="selectedKind === 'user'" class="wx-field">
          <span class="wx-field__label">生日</span>
          <span class="wx-field__value">{{ profile.contact_birthday || "未填写" }}</span>
        </div>
        <div v-if="selectedKind === 'user'" class="wx-field">
          <span class="wx-field__label">个性签名</span>
          <span class="wx-field__value">{{ profile.contact_signature || "未填写" }}</span>
        </div>
        <div v-if="selectedKind === 'group'" class="wx-field">
          <span class="wx-field__label">群公告</span>
          <span class="wx-field__value">{{ profile.notice || "暂无" }}</span>
        </div>
        <div v-if="selectedKind === 'group'" class="wx-field">
          <span class="wx-field__label">成员数</span>
          <span class="wx-field__value">{{ profile.member_cnt || 0 }}</span>
        </div>
        <div v-if="selectedKind === 'group'" class="wx-field">
          <span class="wx-field__label">加群方式</span>
          <span class="wx-field__value">
            {{ Number(profile.add_mode) === 0 ? "直接加入" : "需要审核" }}
          </span>
        </div>
      </div>

      <div class="wx-card">
        <el-button type="primary" style="width: 100%" @click="openChatWithSelected">
          发消息
        </el-button>
        <el-button
          v-if="selectedKind === 'user'"
          type="danger"
          plain
          style="width: 100%; margin: 10px 0 0"
          @click="removeSelectedContact"
        >
          删除联系人
        </el-button>
      </div>
    </div>
  </section>

  <!-- 添加联系人 -->
  <el-dialog v-model="friendDialog" title="添加联系人" width="380px">
    <el-input v-model="friendForm.contact_id" placeholder="对方用户 ID（U 开头）" />
    <el-input
      v-model="friendForm.message"
      placeholder="验证消息（可留空）"
      style="margin-top: 10px"
    />
    <template #footer>
      <el-button @click="friendDialog = false">取消</el-button>
      <el-button type="primary" :loading="friendSubmitting" @click="submitFriend">
        发送申请
      </el-button>
    </template>
  </el-dialog>

  <!-- 新建群聊 -->
  <el-dialog v-model="groupDialog" title="新建群聊" width="400px">
    <el-input v-model="groupForm.name" placeholder="群名称" />
    <el-input
      v-model="groupForm.notice"
      type="textarea"
      :rows="3"
      placeholder="群公告（可留空）"
      style="margin-top: 10px"
    />
    <div class="wx-dialog-row">
      <span class="wx-muted">加群方式</span>
      <el-radio-group v-model="groupForm.add_mode">
        <el-radio :value="0">直接加入</el-radio>
        <el-radio :value="1">需要审核</el-radio>
      </el-radio-group>
    </div>
    <template #footer>
      <el-button @click="groupDialog = false">取消</el-button>
      <el-button type="primary" :loading="groupSubmitting" @click="submitGroup">
        创建
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage, ElMessageBox } from "element-plus";
import WxAvatar from "@/components/WxAvatar.vue";
import {
  applyContact,
  createGroup,
  deleteContact,
  getContactInfo,
  getGroupInfo,
  getNewContactList,
  getUserList,
  loadMyGroup,
  loadMyJoinedGroup,
  passContactApply,
  refuseContactApply,
} from "@/api";

const store = useStore();
const router = useRouter();

const meId = computed(() => store.state.userInfo?.uuid || "");

const keyword = ref("");
const contacts = ref([]);
const groups = ref([]);
const requests = ref([]);
const sections = reactive({ requests: false });

const selectedKey = ref("");
const selectedKind = ref("");
const selectedId = ref("");
const selectedName = ref("");
const selectedAvatar = ref("");
const profile = ref({});
const profileLoading = ref(false);
// 已经取过的资料缓存起来，重复点击同一个联系人不再发请求。
const profileCache = new Map();

const friendDialog = ref(false);
const friendSubmitting = ref(false);
const friendForm = reactive({ contact_id: "", message: "" });

const groupDialog = ref(false);
const groupSubmitting = ref(false);
const groupForm = reactive({ name: "", notice: "", add_mode: 0 });

const filteredContacts = computed(() => {
  const key = keyword.value.trim().toLowerCase();
  if (!key) return contacts.value;
  return contacts.value.filter((item) =>
    String(item.name || item.id || "")
      .toLowerCase()
      .includes(key)
  );
});

const selected = computed(() => !!selectedKey.value);

const applyProfile = (data, kind) => {
  profile.value = data || {};
  if (kind === "user") {
    selectedName.value = data.contact_name || selectedName.value;
    selectedAvatar.value = data.contact_avatar || "";
    return;
  }
  selectedName.value = data.name || selectedName.value;
  selectedAvatar.value = data.avatar || "";
};

const selectContact = async (item) => {
  const key = `user:${item.id}`;
  // 已经是当前选中项就直接返回，避免连点造成重复请求和重复渲染。
  if (selectedKey.value === key && !profileLoading.value) return;

  selectedKey.value = key;
  selectedKind.value = "user";
  selectedId.value = item.id;
  selectedName.value = item.name || item.id;
  selectedAvatar.value = item.avatar || "";
  // 先用列表里已有的数据把右侧面板画出来，网络回来再补全。
  profile.value = {};

  const cached = profileCache.get(key);
  if (cached) {
    applyProfile(cached, "user");
    return;
  }

  profileLoading.value = true;
  try {
    const result = await getContactInfo(item.id);
    if (result.ok && result.data) {
      profileCache.set(key, result.data);
      applyProfile(result.data, "user");
    }
  } finally {
    profileLoading.value = false;
  }
};

const selectGroup = async (item) => {
  const key = `group:${item.group_id}`;
  if (selectedKey.value === key && !profileLoading.value) return;

  selectedKey.value = key;
  selectedKind.value = "group";
  selectedId.value = item.group_id;
  selectedName.value = item.group_name || item.group_id;
  selectedAvatar.value = item.avatar || "";
  profile.value = {};

  const cached = profileCache.get(key);
  if (cached) {
    applyProfile(cached, "group");
    return;
  }

  profileLoading.value = true;
  try {
    const result = await getGroupInfo(item.group_id);
    if (result.ok && result.data) {
      profileCache.set(key, result.data);
      applyProfile(result.data, "group");
    }
  } finally {
    profileLoading.value = false;
  }
};

const loadAll = async () => {
  if (!meId.value) return;
  // 群聊要合并两个来源：我加入的群 + 我创建的群。
  // 后端的 loadMyJoinedGroup 会刻意排除自己当群主的群，只调它会导致群主看不到自己的群。
  const [contactResult, joinedResult, ownedResult, requestResult] = await Promise.all([
    getUserList(meId.value),
    loadMyJoinedGroup(meId.value),
    loadMyGroup(meId.value),
    getNewContactList(meId.value),
  ]);
  if (contactResult.ok && Array.isArray(contactResult.data)) {
    // 接口返回的是 { user_id, user_name, avatar }，这里统一成页面内部使用的 id/name。
    contacts.value = contactResult.data
      .filter((item) => !item.is_deleted)
      .map((item) => ({
        id: item.user_id,
        name: item.user_name,
        avatar: item.avatar,
      }))
      .filter((item) => !!item.id);
  }

  const merged = new Map();
  if (joinedResult.ok && Array.isArray(joinedResult.data)) {
    joinedResult.data.forEach((item) => merged.set(item.group_id, item));
  }
  if (ownedResult.ok && Array.isArray(ownedResult.data)) {
    ownedResult.data.forEach((item) => {
      // 「我创建的群」只返回 id/名称/头像，缺的字段用已加入的那份补齐
      const existing = merged.get(item.group_id) || {};
      merged.set(item.group_id, { ...existing, ...item });
    });
  }
  groups.value = Array.from(merged.values());

  if (requestResult.ok && Array.isArray(requestResult.data)) {
    requests.value = requestResult.data;
  }
};

const toggleSection = (name) => {
  sections[name] = !sections[name];
};

const openChatWithSelected = () => {
  if (!selectedId.value) return;
  router.push({
    name: "ContactChat",
    params: { id: selectedId.value },
    query: {
      name: selectedName.value,
      avatar: selectedAvatar.value,
      kind: selectedKind.value,
    },
  });
};

const acceptRequest = async (item) => {
  const result = await passContactApply(meId.value, item.contact_id);
  if (!result.ok) {
    ElMessage.error(result.message || "操作失败");
    return;
  }
  ElMessage.success("已通过");
  await loadAll();
};

const refuseRequest = async (item) => {
  const result = await refuseContactApply(meId.value, item.contact_id);
  if (!result.ok) {
    ElMessage.error(result.message || "操作失败");
    return;
  }
  ElMessage.success("已忽略");
  await loadAll();
};

const removeSelectedContact = async () => {
  try {
    await ElMessageBox.confirm("删除后聊天记录仍会保留，确定删除？", "删除联系人", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch (error) {
    return;
  }
  const result = await deleteContact(meId.value, selectedId.value);
  if (!result.ok) {
    ElMessage.error(result.message || "删除失败");
    return;
  }
  ElMessage.success("已删除");
  selectedKey.value = "";
  await loadAll();
};

const onCommand = (command) => {
  if (command === "friend") {
    friendForm.contact_id = "";
    friendForm.message = "";
    friendDialog.value = true;
    return;
  }
  groupForm.name = "";
  groupForm.notice = "";
  groupForm.add_mode = 0;
  groupDialog.value = true;
};

const submitFriend = async () => {
  const contactId = friendForm.contact_id.trim();
  if (!contactId) {
    ElMessage.warning("请输入对方用户 ID");
    return;
  }
  friendSubmitting.value = true;
  try {
    const result = await applyContact(meId.value, contactId, friendForm.message.trim());
    if (!result.ok) {
      ElMessage.error(result.message || "申请失败");
      return;
    }
    ElMessage.success("申请已发送");
    friendDialog.value = false;
  } finally {
    friendSubmitting.value = false;
  }
};

const submitGroup = async () => {
  const name = groupForm.name.trim();
  if (!name) {
    ElMessage.warning("请输入群名称");
    return;
  }
  groupSubmitting.value = true;
  try {
    const result = await createGroup({
      owner_id: meId.value,
      name,
      notice: groupForm.notice.trim(),
      add_mode: groupForm.add_mode,
      avatar: "",
    });
    if (!result.ok) {
      ElMessage.error(result.message || "创建失败");
      return;
    }
    ElMessage.success("群聊已创建");
    groupDialog.value = false;
    await loadAll();
  } finally {
    groupSubmitting.value = false;
  }
};

onMounted(loadAll);
</script>

<style scoped>
.wx-row--entry {
  height: 60px;
}
.wx-entry__icon {
  width: 38px;
  height: 38px;
  border-radius: var(--wx-radius);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex: 0 0 auto;
}
.wx-section-title {
  padding: 14px 14px 6px;
  font-size: 12px;
  color: var(--wx-text-2);
}
.wx-section-empty {
  padding: 6px 14px 10px;
  font-size: 12.5px;
  color: var(--wx-text-3);
}
.wx-request-list {
  padding: 4px 8px 8px;
  background: rgba(0, 0, 0, 0.02);
  border-radius: var(--wx-radius);
  margin: 0 8px;
}
.wx-request {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 6px;
}
.wx-request__body {
  flex: 1;
  min-width: 0;
  font-size: 13px;
}
.wx-request-empty {
  padding: 10px;
  font-size: 12.5px;
  color: var(--wx-text-3);
}
.wx-dialog-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 14px;
  font-size: 13px;
}
</style>
