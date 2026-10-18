<template>
  <div class="wx-auth">
    <div class="wx-auth__card">
      <div class="wx-logo">
        <div class="wx-logo__mark">
          <el-icon><ChatDotRound /></el-icon>
        </div>
      </div>
      <div class="wx-auth__title">EchoChat</div>
      <div class="wx-auth__subtitle">账号或手机号登录</div>

      <el-input
        v-model="form.telephone"
        placeholder="手机号 / 账号"
        size="large"
        clearable
        @keyup.enter="submit"
      />
      <el-input
        v-model="form.password"
        type="password"
        placeholder="密码"
        size="large"
        show-password
        @keyup.enter="submit"
      />

      <el-button
        type="primary"
        size="large"
        :loading="loading"
        style="width: 100%; margin-top: 6px"
        @click="submit"
      >
        登录
      </el-button>

      <div class="wx-auth__links">
        <span @click="go('/register')">注册账号</span>
        <span @click="go('/smsLogin')">验证码登录</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage } from "element-plus";
import { login } from "@/api";
import { applyLoginSession } from "@/utils/session";

const router = useRouter();
const store = useStore();
const loading = ref(false);
const form = reactive({ telephone: "", password: "" });

const go = (path) => router.push(path);

const submit = async () => {
  if (loading.value) return;
  if (!form.telephone.trim()) {
    ElMessage.warning("请输入手机号或账号");
    return;
  }
  if (!form.password) {
    ElMessage.warning("请输入密码");
    return;
  }
  loading.value = true;
  try {
    const result = await login(form.telephone.trim(), form.password);
    if (!result.ok) {
      ElMessage.error(result.message || "登录失败");
      return;
    }
    applyLoginSession(store, result.data);
    ElMessage.success("登录成功");
    router.push("/chat/sessionList");
  } finally {
    loading.value = false;
  }
};
</script>

<style scoped>
.wx-logo {
  display: flex;
  justify-content: center;
  margin-bottom: 4px;
}
.wx-logo__mark {
  width: 56px;
  height: 56px;
  border-radius: 14px;
  background: var(--wx-green);
  color: #fff;
  font-size: 30px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 6px 16px rgba(7, 193, 96, 0.28);
}
</style>
