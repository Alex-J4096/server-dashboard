<script setup lang="ts">
import {computed} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {DataAnalysis,Document,EditPen,Monitor,Operation,Platform,User} from '@element-plus/icons-vue'
import {useAuthStore} from './stores/auth'
const route=useRoute(),router=useRouter(),auth=useAuthStore();const title=computed(()=>String(route.meta.title||'Bedrock Control'))
const nav=[['/','服务器概览',Platform],['/console','实时控制台',Monitor],['/config','配置管理',EditPen],['/logs','历史日志',Document],['/metrics','运行指标',DataAnalysis],['/agent','Agent 运维',Operation]] as const
async function logout(){await auth.logout();await router.replace('/login')}
</script>
<template><router-view v-if="route.meta.public"/><div v-else class="shell"><aside class="sidebar"><div class="brand"><span class="brand-mark">B</span><div><strong>BEDROCK</strong><small>CONTROL PANEL</small></div></div><nav><router-link v-for="[to,label,icon] in nav" :key="to" :to="to"><el-icon><component :is="icon"/></el-icon><span>{{label}}</span></router-link><router-link v-if="auth.isAdmin" to="/users"><el-icon><User/></el-icon><span>用户管理</span></router-link></nav><div class="side-foot user-foot"><div><strong>{{auth.user?.username}}</strong><span>{{auth.user?.role}}</span></div><el-button link @click="logout">退出</el-button></div></aside><main><header><div><p>DEFAULT SERVER</p><h1>{{title}}</h1></div><div class="ubuntu">UBUNTU · {{auth.user?.role?.toUpperCase()}}</div></header><section class="content"><router-view/></section></main></div></template>
