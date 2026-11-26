<script setup lang="ts">
import { useThemeStore } from '@/store/modules/theme';
import AuthShell from '../components/auth-shell.vue';
import PwdLogin from './modules/pwd-login.vue';

const themeStore = useThemeStore();
</script>

<template>
  <AuthShell>
    <h3 class="text-18px text-primary font-medium">{{ $t('page.login.pwdLogin.title') }}</h3>
    <div class="pt-24px">
      <Transition :name="themeStore.page.animateMode" mode="out-in" appear>
        <PwdLogin />
      </Transition>
    </div>
  </AuthShell>
</template>

<style scoped></style>
