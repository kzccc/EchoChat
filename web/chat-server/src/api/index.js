// 后端接口封装：统一收敛路径、参数名与响应解包，避免各页面各写一套。
import axios from "axios";

/**
 * 统一解包后端响应：{ code, message, data }。
 * 成功时返回 { ok: true, data }，业务失败时返回 { ok: false, message }，
 * 不抛异常，调用方按布尔值分支即可。
 */
export const call = async (path, payload = {}) => {
  try {
    const response = await axios.post(path, payload);
    const body = response?.data || {};
    if (body.code === 200) {
      return { ok: true, data: body.data, message: body.message || "" };
    }
    return { ok: false, code: body.code, message: body.message || "请求失败" };
  } catch (error) {
    return { ok: false, message: error?.message || "网络异常" };
  }
};

/** 上传接口统一走 multipart，后端只回 message，不回 URL。 */
export const upload = async (path, file) => {
  try {
    const form = new FormData();
    form.append("file", file);
    const response = await axios.post(path, form, {
      headers: { "Content-Type": "multipart/form-data" },
    });
    const body = response?.data || {};
    if (body.code === 200) {
      return { ok: true, message: body.message || "上传成功" };
    }
    return { ok: false, code: body.code, message: body.message || "上传失败" };
  } catch (error) {
    return { ok: false, message: error?.message || "上传失败" };
  }
};

/* ---------------- 认证 ---------------- */
export const login = (telephone, password) =>
  call("/login", { telephone, password });

export const smsLogin = (telephone, sms_code) =>
  call("/user/smsLogin", { telephone, sms_code });

export const register = (payload) => call("/register", payload);

export const sendSmsCode = (telephone) =>
  call("/user/sendSmsCode", { telephone });

/* ---------------- 用户 ---------------- */
export const getUserInfo = (uuid) => call("/user/getUserInfo", { uuid });

export const updateUserInfo = (payload) =>
  call("/user/updateUserInfo", payload);

export const wsLogout = (owner_id) => call("/user/wsLogout", { owner_id });

export const uploadAvatar = (file) => upload("/message/uploadAvatar", file);

/* ---------------- 会话 ---------------- */
export const getUserSessionList = (owner_id) =>
  call("/session/getUserSessionList", { owner_id });

export const getGroupSessionList = (owner_id) =>
  call("/session/getGroupSessionList", { owner_id });

export const openSession = (send_id, receive_id) =>
  call("/session/openSession", { send_id, receive_id });

export const deleteSession = (owner_id, session_id) =>
  call("/session/deleteSession", { owner_id, session_id });

export const markRead = (conversation_key, last_read_seq) =>
  call("/session/markRead", { conversation_key, last_read_seq });

/* ---------------- 联系人 ---------------- */
export const getUserList = (owner_id) =>
  call("/contact/getUserList", { owner_id });

export const loadMyJoinedGroup = (owner_id) =>
  call("/contact/loadMyJoinedGroup", { owner_id });

export const getContactInfo = (contact_id) =>
  call("/contact/getContactInfo", { contact_id });

export const deleteContact = (owner_id, contact_id) =>
  call("/contact/deleteContact", { owner_id, contact_id });

export const applyContact = (owner_id, contact_id, message) =>
  call("/contact/applyContact", { owner_id, contact_id, message });

export const getNewContactList = (owner_id) =>
  call("/contact/getNewContactList", { owner_id });

export const passContactApply = (owner_id, contact_id) =>
  call("/contact/passContactApply", { owner_id, contact_id });

export const refuseContactApply = (owner_id, contact_id) =>
  call("/contact/refuseContactApply", { owner_id, contact_id });

export const blackContact = (owner_id, contact_id) =>
  call("/contact/blackContact", { owner_id, contact_id });

export const cancelBlackContact = (owner_id, contact_id) =>
  call("/contact/cancelBlackContact", { owner_id, contact_id });

export const blackApply = (owner_id, contact_id) =>
  call("/contact/blackApply", { owner_id, contact_id });

/* ---------------- 群聊 ---------------- */
export const createGroup = (payload) => call("/group/createGroup", payload);

export const loadMyGroup = (owner_id) => call("/group/loadMyGroup", { owner_id });

export const checkGroupAddMode = (group_id) =>
  call("/group/checkGroupAddMode", { group_id });

export const enterGroupDirectly = (owner_id, contact_id) =>
  call("/group/enterGroupDirectly", { owner_id, contact_id });

export const leaveGroup = (user_id, group_id) =>
  call("/group/leaveGroup", { user_id, group_id });

export const dismissGroup = (owner_id, group_id) =>
  call("/group/dismissGroup", { owner_id, group_id });

export const getGroupInfo = (group_id) =>
  call("/group/getGroupInfo", { group_id });

export const updateGroupInfo = (payload) =>
  call("/group/updateGroupInfo", payload);

export const getGroupMemberList = (group_id) =>
  call("/group/getGroupMemberList", { group_id });

export const removeGroupMembers = (group_id, owner_id, uuid_list) =>
  call("/group/removeGroupMembers", { group_id, owner_id, uuid_list });

export const getAddGroupList = (group_id) =>
  call("/contact/getAddGroupList", { group_id });

/* ---------------- 消息 ---------------- */
export const getMessageList = (user_one_id, user_two_id, before_seq = 0, limit = 30) =>
  call("/message/getMessageList", {
    user_one_id,
    user_two_id,
    before_seq,
    limit,
  });

export const getGroupMessageList = (group_id, before_seq = 0, limit = 30) =>
  call("/message/getGroupMessageList", { group_id, before_seq, limit });

export const uploadChatFile = (file) => upload("/message/uploadFile", file);

export const getCurContactListInChatRoom = (owner_id, contact_id) =>
  call("/chatroom/getCurContactListInChatRoom", { owner_id, contact_id });

/* ---------------- 后台管理 ---------------- */
export const adminGetUserInfoList = (owner_id) =>
  call("/user/getUserInfoList", { owner_id });

export const adminAbleUsers = (uuid_list) =>
  call("/user/ableUsers", { uuid_list });

export const adminDisableUsers = (uuid_list) =>
  call("/user/disableUsers", { uuid_list });

export const adminDeleteUsers = (uuid_list) =>
  call("/user/deleteUsers", { uuid_list });

export const adminSetAdmin = (uuid_list, is_admin) =>
  call("/user/setAdmin", { uuid_list, is_admin });

export const adminGetGroupInfoList = () => call("/group/getGroupInfoList", {});

export const adminSetGroupsStatus = (uuid_list, status) =>
  call("/group/setGroupsStatus", { uuid_list, status });

export const adminDeleteGroups = (uuid_list) =>
  call("/group/deleteGroups", { uuid_list });

export const adminDLQList = (payload) => call("/dlq/list", payload);
export const adminDLQDetail = (id) => call("/dlq/detail", { id });
export const adminDLQClaim = (id) => call("/dlq/claim", { id });
export const adminDLQReopen = (id) => call("/dlq/reopen", { id });
export const adminDLQClose = (id, close_reason, remark) =>
  call("/dlq/close", { id, close_reason, remark });
export const adminDLQRemark = (id, remark) => call("/dlq/remark", { id, remark });
