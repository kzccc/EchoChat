<template>
  <div class="wx-auth">
    <div class="wx-auth__card">
      <div class="wx-logo">
        <div class="wx-logo__mark">
          <el-icon><ChatDotRound /></el-icon>
        </div>
      </div>
      <div class="wx-auth__title">注册账号</div>
      <div class="wx-auth__subtitle">手机号 + 验证码</div>

      <el-input v-model="form.telephone" placeholder="手机号" size="large" clearable />
      <el-input v-model="form.nickname" placeholder="昵称" size="large" />
      <el-input
        v-model="form.password"
        type="password"
        placeholder="密码（至少 6 位）"
        size="large"
        show-password
      />

      <div class="wx-code-row">
        <el-input
          v-model="form.sms_code"
          placeholder="短信验证码"
          size="large"
          @keyup.enter="submit"
        />
        <el-button
          size="large"
          :disabled="countdown > 0 || sending"
          :loading="sending"
          @click="sendCode"
        >
          {{ countdown > 0 ? `${countdown}s` : "获取验证码" }}
        </el-button>
      </div>

      <el-button
        type="primary"
        size="large"
        :loading="loading"
        style="width: 100%; margin-top: 6px"
        @click="submit"
      >
        注册并登录
      </el-button>

      <div class="wx-auth__links">
        <span @click="go('/login')">已有账号，去登录</span>
        <span @click="go('/smsLogin')">验证码登录</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onBeforeUnmount, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { useStore } from "vuex";
import { ElMessage } from "element-plus";
import { register, sendSmsCode } from "@/api";
import { applyLoginSession } from "@/utils/session";

const router = useRouter();
const store = useStore();

const loading = ref(false);
const sending = ref(false);
const countdown = ref(0);
let timer = null;

const form = reactive({
  telephone: "",
  nickname: "",
  password: "",
  sms_code: "",
});

const go = (path) => router.push(path);

const startCountdown = () => {
  countdown.value = 60;
  timer = setInterval(() => {
    countdown.value -= 1;
    if (countdown.value <= 0) {
      clearInterval(timer);
      timer = null;
    }
  }, 1000);
};

const sendCode = async () => {
  if (!form.telephone.trim()) {
    ElMessage.warning("请先输入手机号");
    return;
  }
  sending.value = true;
  try {
    const result = await sendSmsCode(form.telephone.trim());
    if (!result.ok) {
      ElMessage.error(result.message || "验证码发送失败");
      return;
    }
    ElMessage.success("验证码已发送");
    startCountdown();
  } finally {
    sending.value = false;
  }
};

const submit = async () => {
  if (loading.value) return;
  if (!form.telephone.trim()) {
    ElMessage.warning("请输入手机号");
    return;
  }
  if (!form.nickname.trim()) {
    ElMessage.warning("请输入昵称");
    return;
  }
  if (!form.password || form.password.length < 6) {
    ElMessage.warning("密码至少 6 位");
    return;
  }
  if (!form.sms_code.trim()) {
    ElMessage.warning("请输入短信验证码");
    return;
  }
  loading.value = true;
  try {
    const result = await register({
      telephone: form.telephone.trim(),
      nickname: form.nickname.trim(),
      password: form.password,
      sms_code: form.sms_code.trim(),
    });
    if (!result.ok) {
      ElMessage.error(result.message || "注册失败");
      return;
    }
    applyLoginSession(store, result.data);
    ElMessage.success("注册成功");
    router.push("/chat/sessionList");
  } finally {
    loading.value = false;
  }
};

onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});
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
.wx-code-row {
  display: flex;
  gap: 10px;
}
.wx-code-row .el-button {
  flex: 0 0 auto;
}
</style>
