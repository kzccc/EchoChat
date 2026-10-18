<template>
  <div
    class="wx-avatar"
    :class="{ 'wx-avatar--round': round }"
    :style="avatarStyle"
  >
    <img
      v-if="resolvedSrc"
      :src="resolvedSrc"
      :alt="name"
      loading="lazy"
      decoding="async"
      referrerpolicy="no-referrer"
      @error="onError"
    />
    <span v-else>{{ initials }}</span>
  </div>
</template>

<script setup>
import { computed, ref, watch } from "vue";
import { useStore } from "vuex";
import { avatarText, isPlaceholderAvatar, resolveAssetUrl } from "@/utils/format";

const props = defineProps({
  name: { type: String, default: "" },
  src: { type: String, default: "" },
  size: { type: Number, default: 40 },
  round: { type: Boolean, default: false },
});

const store = useStore();
const failed = ref(false);

watch(
  () => props.src,
  () => {
    failed.value = false;
  }
);

const resolvedSrc = computed(() => {
  if (failed.value) return "";
  // 默认占位图不发起网络请求，直接用首字渲染。
  if (isPlaceholderAvatar(props.src)) return "";
  return resolveAssetUrl(store.state.backendUrl, props.src);
});

const initials = computed(() => avatarText(props.name));

// 用名字做一次稳定哈希，给没有头像的用户一个固定的低饱和背景色。
const palette = [
  "#7ecb9b",
  "#6fbfd8",
  "#9b9be0",
  "#d8a26f",
  "#d88fa8",
  "#7fb2a8",
  "#b0a06f",
];
const background = computed(() => {
  const value = String(props.name || "?");
  let hash = 0;
  for (let i = 0; i < value.length; i += 1) {
    hash = (hash * 31 + value.charCodeAt(i)) % 9973;
  }
  return palette[hash % palette.length];
});

const avatarStyle = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  fontSize: `${Math.max(11, Math.round(props.size * 0.38))}px`,
  background: resolvedSrc.value ? "#e6e6e6" : background.value,
}));

const onError = () => {
  failed.value = true;
};
</script>
