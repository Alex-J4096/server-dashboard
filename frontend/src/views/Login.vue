<script setup lang="ts">
import {reactive,ref} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {ElMessage} from 'element-plus'
import {useAuthStore} from '../stores/auth'
const auth=useAuthStore(),route=useRoute(),router=useRouter(),loading=ref(false)
const form=reactive({username:'',password:''})
async function submit(){if(!form.username||!form.password)return;loading.value=true;try{await auth.login(form.username,form.password);await router.replace(String(route.query.redirect||'/'))}catch(e){ElMessage.error((e as Error).message)}finally{loading.value=false}}
</script>
<template><div class="login-page"><form class="login-card" @submit.prevent="submit"><div class="brand login-brand"><span class="brand-mark">B</span><div><strong>BEDROCK</strong><small>CONTROL PANEL</small></div></div><h1>登入管理面板</h1><p class="muted">使用管理员分配的账户继续。</p><el-input v-model="form.username" size="large" autocomplete="username" placeholder="用户名" autofocus/><el-input v-model="form.password" size="large" type="password" show-password autocomplete="current-password" placeholder="密码"/><el-button native-type="submit" type="success" size="large" :loading="loading" :disabled="!form.username||!form.password">登入</el-button></form></div></template>
