<template>
  <section class="wx-page">
    <div class="wx-page__header">我的</div>

    <div class="wx-profile">
      <div class="wx-profile__avatar" @click="pickAvatar">
        <wx-avatar :name="nickname" :src="avatar" :size="64" />
        <div class="wx-profile__avatar-mask">更换</div>
      </div>
      <div>
        <div class="wx-profile__name">{{ nickname || "未设置昵称" }}</div>
        <div class="wx-profile__meta">
          {{ userInfo.telephone || "未绑定手机号" }}
          <span v-if="isAdmin"> · 管理员</span>
        </div>
      </div>
    </div>

    <div class="wx-card">
      <div class="wx-card__title">个人资料</div>

      <div class="wx-field">
        <span class="wx-field__label">昵称</span>
        <div class="wx-field__value">
          <el-input v-model="form.nickname" placeholder="昵称" />
        </div>
      </div>

      <div class="wx-field">
        <span class="wx-field__label">邮箱</span>
        <div class="wx-field__value">
          <el-input v-model="form.email" placeholder="邮箱" />
        </div>
      </div>

      <div class="wx-field">
        <span class="wx-field__label">生日</span>
        <div class="wx-field__value">
          <el-date-picker
            v-model="form.birthday"
            type="date"
            value-format="YYYY-MM-DD"
            placeholder="选择日期"
            style="width: 100%"
          />
        </div>
      </div>

      <div class="wx-field">
        <span class="wx-field__label">签名</span>
        <div class="wx-field__value">
          <el-input
            v-model="form.signature"
            type="textarea"
            :rows="2"
            maxlength="60"
            show-word-limit
            placeholder="一句话介绍自己"
          />
        </div>
      </div>

      <el-button
        type="primary"
        style="width: 100%; margin-top: 18px"
        :loading="saving"
        @click="save"
      >
        保存
      </el-button>
    </div>

    <div class="wx-card">
      <div class="wx-card__title">账号</div>
      <div class="wx-field">
        <span class="wx-field__label">用户 ID</span>
        <span class="wx-field__value wx-ellipsis">{{ userInfo.uuid || "-" }}</span>
        <el-button link size="small" @click="copyId">复制</el-button>
      </div>
      <div class="wx-field">
        <span class="wx-field__label">注册时间</span>
        <span class="wx-field__value">{{ userInfo.created_at || "-" }}</span>
      </div>
      <el-button
        v-if="isAdmin"
        style="width: 100%; margin-top: 14px"
        @click="goManager"
      >
        进入后台管理
      </el-button>
      <el-button
        type="danger"
        plain
        style="width: 100%; margin: 10px 0 0"
        @click="handleLogout"
      >
        退出登录
      </el-button>
    </div>

    <input
      ref="avatarInputRef"
      type="file"
      accept="image/*"
      style="display: none"
      @change="onAvatarPicked"
    />
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage } from "element-plus";
import WxAvatar from "@/components/WxAvatar.vue";
import { updateUserInfo, uploadAvatar } from "@/api";
import { logoutSession, normalizeUserInfo } from "@/utils/session";

const store = useStore();
const router = useRouter();

const saving = ref(false);
const avatarInputRef = ref(null);

const userInfo = computed(() => store.state.userInfo || {});
const nickname = computed(() => userInfo.value.nickname || "");
const avatar = computed(() => userInfo.value.avatar || "");
const isAdmin = computed(() => Number(userInfo.value.is_admin) === 1);

const form = reactive({
  nickname: "",
  email: "",
  birthday: "",
  signature: "",
});

const fillForm = () => {
  form.nickname = userInfo.value.nickname || "";
  form.email = userInfo.value.email || "";
  form.birthday = userInfo.value.birthday || "";
  form.signature = userInfo.value.signature || "";
};

const save = async () => {
  if (!userInfo.value.uuid) return;
  if (!form.nickname.trim()) {
    ElMessage.warning("昵称不能为空");
    return;
  }
  saving.value = true;
  try {
    const result = await updateUserInfo({
      uuid: userInfo.value.uuid,
      nickname: form.nickname.trim(),
      email: form.email.trim(),
      birthday: form.birthday || "",
      signature: form.signature.trim(),
      avatar: userInfo.value.avatar || "",
    });
    if (!result.ok) {
      ElMessage.error(result.message || "保存失败");
      return;
    }
    store.commit(
      "setUserInfo",
      normalizeUserInfo(store, {
        ...userInfo.value,
        nickname: form.nickname.trim(),
        email: form.email.trim(),
        birthday: form.birthday || "",
        signature: form.signature.trim(),
      })
    );
    ElMessage.success("已保存");
  } finally {
    saving.value = false;
  }
};

const pickAvatar = () => avatarInputRef.value?.click();

const onAvatarPicked = async (event) => {
  const file = event.target.files?.[0];
  event.target.value = "";
  if (!file) return;
  ElMessage.info("正在上传头像…");
  const uploaded = await uploadAvatar(file);
  if (!uploaded.ok) {
    ElMessage.error(uploaded.message || "上传失败");
    return;
  }
  // 后端按原始文件名保存到静态头像目录，这里拼出可访问路径再写回资料。
  const path = `/static/avatars/${file.name}`;
  const result = await updateUserInfo({
    uuid: userInfo.value.uuid,
    nickname: form.nickname.trim() || userInfo.value.nickname || "",
    email: form.email.trim(),
    birthday: form.birthday || "",
    signature: form.signature.trim(),
    avatar: path,
  });
  if (!result.ok) {
    ElMessage.error(result.message || "头像保存失败");
    return;
  }
  store.commit(
    "setUserInfo",
    normalizeUserInfo(store, { ...userInfo.value, avatar: path })
  );
  ElMessage.success("头像已更新");
};

const copyId = async () => {
  try {
    await navigator.clipboard.writeText(userInfo.value.uuid || "");
    ElMessage.success("已复制用户 ID");
  } catch (error) {
    ElMessage.warning("当前浏览器不允许自动复制，请手动选择");
  }
};

const goManager = () => router.push("/manager");

const handleLogout = async () => {
  await logoutSession(store);
  router.push("/login");
};

onMounted(fillForm);
</script>

<style scoped>
.wx-profile__avatar {
  position: relative;
  cursor: pointer;
  border-radius: var(--wx-radius);
  overflow: hidden;
}
.wx-profile__avatar-mask {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  font-size: 12px;
  opacity: 0;
  transition: opacity 0.15s;
}
.wx-profile__avatar:hover .wx-profile__avatar-mask {
  opacity: 1;
}
</style>
