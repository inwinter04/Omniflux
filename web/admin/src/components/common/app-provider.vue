<script setup lang="ts">
import { createTextVNode, defineComponent, onMounted } from 'vue';
import { useDialog, useLoadingBar, useMessage, useNotification } from 'naive-ui';
import { request } from '@/service/request';

defineOptions({
  name: 'AppProvider'
});

const ContextHolder = defineComponent({
  name: 'ContextHolder',
  setup() {
    function register() {
      window.$loadingBar = useLoadingBar();
      window.$dialog = useDialog();
      window.$message = useMessage();
      window.$notification = useNotification();
    }

    register();

    // 应用挂载前就发现会话失效时,消息容器还不存在,请求层先把提示记下,这里补显示
    onMounted(() => {
      const { pendingNotice } = request.state;

      if (pendingNotice) {
        request.state.pendingNotice = '';
        window.$message?.warning(pendingNotice, { duration: 5000 });
      }
    });

    return () => createTextVNode();
  }
});
</script>

<template>
  <NLoadingBarProvider>
    <NDialogProvider>
      <NNotificationProvider>
        <NMessageProvider>
          <ContextHolder />
          <slot></slot>
        </NMessageProvider>
      </NNotificationProvider>
    </NDialogProvider>
  </NLoadingBarProvider>
</template>

<style scoped></style>
