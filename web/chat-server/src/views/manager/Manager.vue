<template>
  <section class="wx-page">
    <div class="wx-page__header">
      后台管理
      <span class="wx-muted" style="margin-left: 10px; font-size: 12px; font-weight: 400">
        仅管理员可见
      </span>
    </div>

    <el-tabs v-model="activeTab" class="wx-manager__tabs" @tab-change="onTabChange">
      <!-- 用户 -->
      <el-tab-pane label="用户" name="users">
        <div class="wx-manager__toolbar">
          <el-input
            v-model="userKeyword"
            placeholder="按昵称 / 手机号 / ID 过滤"
            clearable
            style="width: 260px"
          />
          <el-button :loading="loadingUsers" @click="loadUsers">刷新</el-button>
        </div>

        <el-table :data="filteredUsers" size="small" height="calc(100vh - 220px)">
          <el-table-column prop="nickname" label="昵称" min-width="140" show-overflow-tooltip />
          <el-table-column prop="uuid" label="用户 ID" min-width="180" show-overflow-tooltip />
          <el-table-column prop="telephone" label="手机号" width="130" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="Number(row.status) === 0 ? 'success' : 'info'" size="small">
                {{ Number(row.status) === 0 ? "正常" : "已禁用" }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="管理员" width="90">
            <template #default="{ row }">
              <el-tag v-if="Number(row.is_admin) === 1" type="warning" size="small">是</el-tag>
              <span v-else class="wx-muted">否</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="260" fixed="right">
            <template #default="{ row }">
              <el-button
                link
                size="small"
                @click="toggleUserStatus(row)"
              >
                {{ Number(row.status) === 0 ? "禁用" : "启用" }}
              </el-button>
              <el-button link size="small" @click="toggleAdmin(row)">
                {{ Number(row.is_admin) === 1 ? "取消管理员" : "设为管理员" }}
              </el-button>
              <el-button link size="small" type="danger" @click="removeUser(row)">
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 群聊 -->
      <el-tab-pane label="群聊" name="groups">
        <div class="wx-manager__toolbar">
          <el-button :loading="loadingGroups" @click="loadGroups">刷新</el-button>
        </div>

        <el-table :data="groups" size="small" height="calc(100vh - 220px)">
          <el-table-column prop="name" label="群名称" min-width="180" show-overflow-tooltip />
          <el-table-column prop="uuid" label="群 ID" min-width="180" show-overflow-tooltip />
          <el-table-column prop="owner_id" label="群主" min-width="160" show-overflow-tooltip />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="Number(row.status) === 0 ? 'success' : 'info'" size="small">
                {{ groupStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="220" fixed="right">
            <template #default="{ row }">
              <el-button link size="small" @click="toggleGroupStatus(row)">
                {{ Number(row.status) === 0 ? "停用" : "启用" }}
              </el-button>
              <el-button link size="small" type="danger" @click="removeGroup(row)">
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 死信队列 -->
      <el-tab-pane label="死信队列" name="dlq">
        <div class="wx-manager__toolbar">
          <el-input v-model="dlqFilter.message_id" placeholder="消息 ID" clearable style="width: 200px" />
          <el-input v-model="dlqFilter.stage" placeholder="失败阶段" clearable style="width: 150px" />
          <el-select v-model="dlqFilter.status" placeholder="状态" clearable style="width: 130px">
            <el-option label="待处理" value="PENDING" />
            <el-option label="重试中" value="RETRYING" />
            <el-option label="人工处理" value="MANUAL" />
            <el-option label="已完成" value="DONE" />
          </el-select>
          <el-button type="primary" :loading="loadingDlq" @click="loadDlq">查询</el-button>
        </div>

        <el-table :data="dlqList" size="small" height="calc(100vh - 260px)">
          <el-table-column prop="id" label="ID" width="80" />
          <el-table-column prop="message_id" label="消息 ID" min-width="180" show-overflow-tooltip />
          <el-table-column prop="stage" label="阶段" width="150" show-overflow-tooltip />
          <el-table-column prop="failure_type" label="失败类型" width="120" />
          <el-table-column label="自动状态" width="100">
            <template #default="{ row }">
              <el-tag :type="autoStatusTagType(row.status)" size="small" effect="plain">
                {{ autoStatusText(row.status) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="人工状态" width="150">
            <template #default="{ row }">
              <el-tag :type="manualStatusTagType(row.manual_status)" size="small">
                {{ manualStatusText(row.manual_status) }}
              </el-tag>
              <div v-if="row.close_reason" class="wx-dlq__hint">
                {{ closeReasonText(row.close_reason) }}
              </div>
              <div v-else-if="row.assignee" class="wx-dlq__hint">
                {{ row.assignee }}
              </div>
            </template>
          </el-table-column>
          <el-table-column label="重试" width="80">
            <template #default="{ row }">
              {{ row.attempt_count }}/{{ row.max_attempt_count }}
            </template>
          </el-table-column>
          <el-table-column prop="last_error" label="最后错误" min-width="220" show-overflow-tooltip />
          <el-table-column label="操作" width="240" fixed="right">
            <template #default="{ row }">
              <el-button
                link
                size="small"
                :disabled="!canClaim(row)"
                :title="claimDisabledHint(row)"
                @click="claimDlq(row)"
              >
                领取
              </el-button>
              <el-button
                link
                size="small"
                :disabled="!canReopen(row)"
                title="只有已关闭的记录才能重开"
                @click="reopenDlq(row)"
              >
                重开
              </el-button>
              <el-button
                link
                size="small"
                type="danger"
                :disabled="!canClose(row)"
                :title="closeDisabledHint(row)"
                @click="openCloseDialog(row)"
              >
                关闭
              </el-button>
            </template>
          </el-table-column>
        </el-table>

        <div class="wx-manager__pager">
          <el-pagination
            layout="total, prev, pager, next"
            :total="dlqTotal"
            :page-size="dlqFilter.page_size"
            :current-page="dlqFilter.page"
            @current-change="onDlqPageChange"
          />
        </div>

        <!-- 关闭死信：必须选一个合法原因，备注可选 -->
        <el-dialog v-model="closeDialog" title="关闭死信记录" width="420px">
          <div class="wx-dialog-line">
            <span class="wx-muted">记录 ID</span>
            <span>{{ closeForm.id }}</span>
          </div>
          <div class="wx-dialog-line wx-dialog-line--block">
            <span class="wx-muted">关闭原因（必选）</span>
            <el-select v-model="closeForm.reason" placeholder="请选择" style="width: 100%">
              <el-option label="已丢弃：这条消息不再处理" value="discarded" />
              <el-option label="外部已修复：问题在别处修好了" value="externally_fixed" />
              <el-option label="预期内失败：本来就会失败，不是故障" value="expected" />
              <el-option label="合并故障单：并入其它事件统一处理" value="merged_incident" />
            </el-select>
          </div>
          <div class="wx-dialog-line wx-dialog-line--block">
            <span class="wx-muted">备注（可选）</span>
            <el-input
              v-model="closeForm.remark"
              type="textarea"
              :rows="3"
              placeholder="例如：已确认上游重复投递，无需补偿"
            />
          </div>
          <template #footer>
            <el-button @click="closeDialog = false">取消</el-button>
            <el-button
              type="danger"
              :loading="closeSubmitting"
              :disabled="!closeForm.reason"
              @click="submitCloseDlq"
            >
              确认关闭
            </el-button>
          </template>
        </el-dialog>
      </el-tab-pane>
    </el-tabs>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from "vue";
import { useStore } from "vuex";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  adminAbleUsers,
  adminDeleteGroups,
  adminDeleteUsers,
  adminDisableUsers,
  adminDLQClaim,
  adminDLQClose,
  adminDLQList,
  adminDLQReopen,
  adminGetGroupInfoList,
  adminGetUserInfoList,
  adminSetAdmin,
  adminSetGroupsStatus,
} from "@/api";

const store = useStore();
const meId = computed(() => store.state.userInfo?.uuid || "");

const activeTab = ref("users");

/* ---------------- 用户 ---------------- */

const users = ref([]);
const loadingUsers = ref(false);
const userKeyword = ref("");

const filteredUsers = computed(() => {
  const key = userKeyword.value.trim().toLowerCase();
  if (!key) return users.value;
  return users.value.filter((item) =>
    [item.nickname, item.telephone, item.uuid]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(key))
  );
});

const loadUsers = async () => {
  loadingUsers.value = true;
  try {
    const result = await adminGetUserInfoList(meId.value);
    if (!result.ok) {
      ElMessage.error(result.message || "加载用户失败");
      return;
    }
    users.value = Array.isArray(result.data) ? result.data : [];
  } finally {
    loadingUsers.value = false;
  }
};

const toggleUserStatus = async (row) => {
  const disable = Number(row.status) === 0;
  const result = disable
    ? await adminDisableUsers([row.uuid])
    : await adminAbleUsers([row.uuid]);
  if (!result.ok) {
    ElMessage.error(result.message || "操作失败");
    return;
  }
  ElMessage.success(disable ? "已禁用" : "已启用");
  await loadUsers();
};

const toggleAdmin = async (row) => {
  const next = Number(row.is_admin) === 1 ? 0 : 1;
  const result = await adminSetAdmin([row.uuid], next);
  if (!result.ok) {
    ElMessage.error(result.message || "操作失败");
    return;
  }
  ElMessage.success(next === 1 ? "已设为管理员" : "已取消管理员");
  await loadUsers();
};

const removeUser = async (row) => {
  try {
    await ElMessageBox.confirm(`删除用户「${row.nickname || row.uuid}」？`, "删除用户", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch (error) {
    return;
  }
  const result = await adminDeleteUsers([row.uuid]);
  if (!result.ok) {
    ElMessage.error(result.message || "删除失败");
    return;
  }
  ElMessage.success("已删除");
  await loadUsers();
};

/* ---------------- 群聊 ---------------- */

const groups = ref([]);
const loadingGroups = ref(false);

const groupStatusText = (status) => {
  switch (Number(status)) {
    case 0:
      return "正常";
    case 1:
      return "已停用";
    default:
      return "已解散";
  }
};

const loadGroups = async () => {
  loadingGroups.value = true;
  try {
    const result = await adminGetGroupInfoList();
    if (!result.ok) {
      ElMessage.error(result.message || "加载群聊失败");
      return;
    }
    groups.value = Array.isArray(result.data) ? result.data : [];
  } finally {
    loadingGroups.value = false;
  }
};

const toggleGroupStatus = async (row) => {
  const next = Number(row.status) === 0 ? 1 : 0;
  const result = await adminSetGroupsStatus([row.uuid], next);
  if (!result.ok) {
    ElMessage.error(result.message || "操作失败");
    return;
  }
  ElMessage.success(next === 1 ? "已停用" : "已启用");
  await loadGroups();
};

const removeGroup = async (row) => {
  try {
    await ElMessageBox.confirm(`删除群聊「${row.name || row.uuid}」？`, "删除群聊", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch (error) {
    return;
  }
  const result = await adminDeleteGroups([row.uuid]);
  if (!result.ok) {
    ElMessage.error(result.message || "删除失败");
    return;
  }
  ElMessage.success("已删除");
  await loadGroups();
};

/* ---------------- 死信 ---------------- */

const dlqList = ref([]);
const dlqTotal = ref(0);
const loadingDlq = ref(false);
const dlqFilter = reactive({
  message_id: "",
  stage: "",
  status: "",
  page: 1,
  page_size: 20,
});

/* DLQ 有两套状态：status 是自动治理，manual_status 是人工治理，要分开显示 */
const autoStatusText = (status) => {
  switch (String(status)) {
    case "pending":
      return "待重试";
    case "retrying":
      return "重试中";
    case "done":
      return "已完成";
    case "manual":
      return "转人工";
    default:
      return String(status || "-");
  }
};

const autoStatusTagType = (status) => {
  switch (String(status)) {
    case "done":
      return "success";
    case "manual":
      return "warning";
    case "retrying":
      return "primary";
    default:
      return "info";
  }
};

const manualStatusText = (manualStatus) => {
  switch (String(manualStatus)) {
    case "open":
      return "待处理";
    case "in_progress":
      return "处理中";
    case "closed":
      return "已关闭";
    default:
      return String(manualStatus || "-");
  }
};

const manualStatusTagType = (manualStatus) => {
  switch (String(manualStatus)) {
    case "open":
      return "warning";
    case "in_progress":
      return "primary";
    case "closed":
      return "info";
    default:
      return "info";
  }
};

const closeReasonText = (reason) => {
  switch (String(reason)) {
    case "discarded":
      return "原因：已丢弃";
    case "externally_fixed":
      return "原因：外部已修复";
    case "expected":
      return "原因：预期内失败";
    case "merged_incident":
      return "原因：合并故障单";
    default:
      return String(reason || "");
  }
};

const loadDlq = async () => {
  loadingDlq.value = true;
  try {
    const result = await adminDLQList({
      message_id: dlqFilter.message_id.trim(),
      stage: dlqFilter.stage.trim(),
      status: dlqFilter.status || "",
      page: dlqFilter.page,
      page_size: dlqFilter.page_size,
    });
    if (!result.ok) {
      ElMessage.error(result.message || "加载死信失败");
      return;
    }
    dlqList.value = Array.isArray(result.data?.list) ? result.data.list : [];
    dlqTotal.value = Number(result.data?.total || 0);
  } finally {
    loadingDlq.value = false;
  }
};

const onDlqPageChange = (page) => {
  dlqFilter.page = page;
  loadDlq();
};

const claimDlq = async (row) => {
  const result = await adminDLQClaim(row.id);
  if (!result.ok) {
    ElMessage.error(result.message || "领取失败");
    return;
  }
  ElMessage.success("已领取");
  await loadDlq();
};

const reopenDlq = async (row) => {
  const result = await adminDLQReopen(row.id);
  if (!result.ok) {
    ElMessage.error(result.message || "重开失败");
    return;
  }
  ElMessage.success("已重开");
  await loadDlq();
};

/* 三个动作各有前置条件，前端按同样规则置灰，避免点了才报错 */
const isManualPool = (row) => String(row.status) === "manual";
const isClosed = (row) => String(row.manual_status) === "closed";

const canClaim = (row) => isManualPool(row) && !isClosed(row);
const canReopen = (row) => isManualPool(row) && isClosed(row);
const canClose = (row) => isManualPool(row) && !isClosed(row);

const claimDisabledHint = (row) => {
  if (!isManualPool(row)) return "仍处于自动治理流程，不能被人工接手";
  if (isClosed(row)) return "已关闭，请先重开";
  return "认领这条记录，标记为处理中";
};

const closeDisabledHint = (row) => {
  if (!isManualPool(row)) return "仍处于自动治理流程，无需人工关闭";
  if (isClosed(row)) return "已经关闭过了";
  return "人工给出终态结论";
};

const closeDialog = ref(false);
const closeSubmitting = ref(false);
const closeForm = reactive({ id: 0, reason: "", remark: "" });

const openCloseDialog = (row) => {
  closeForm.id = row.id;
  closeForm.reason = "";
  closeForm.remark = "";
  closeDialog.value = true;
};

const submitCloseDlq = async () => {
  if (!closeForm.reason) {
    ElMessage.warning("请选择关闭原因");
    return;
  }
  closeSubmitting.value = true;
  try {
    const result = await adminDLQClose(
      closeForm.id,
      closeForm.reason,
      closeForm.remark.trim()
    );
    if (!result.ok) {
      ElMessage.error(result.message || "关闭失败");
      return;
    }
    ElMessage.success("已关闭");
    closeDialog.value = false;
    await loadDlq();
  } finally {
    closeSubmitting.value = false;
  }
};

/* ---------------- 生命周期 ---------------- */

const onTabChange = (name) => {
  if (name === "users" && !users.value.length) loadUsers();
  if (name === "groups" && !groups.value.length) loadGroups();
  if (name === "dlq" && !dlqList.value.length) loadDlq();
};

onMounted(loadUsers);
</script>

<style scoped>
.wx-manager__tabs {
  padding: 0 16px 16px;
  height: 100%;
  overflow: auto;
}
.wx-manager__toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.wx-manager__pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.wx-dialog-line {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 13.5px;
  margin-bottom: 14px;
}
.wx-dialog-line--block {
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
}
.wx-dlq__hint {
  margin-top: 3px;
  font-size: 11px;
  color: var(--wx-text-3);
  line-height: 1.4;
}
</style>
