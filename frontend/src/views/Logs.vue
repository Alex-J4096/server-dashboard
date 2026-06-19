<script setup lang="ts">
import {onMounted,ref} from 'vue'
import {ElMessage} from 'element-plus'
import {api} from '../api/client'
import type {LogEntry} from '../api/types'
const query=ref(''),rows=ref<LogEntry[]>([]),loading=ref(false)
async function load(){loading.value=true;try{rows.value=await api(`/api/logs${query.value?'/search':''}?limit=500&q=${encodeURIComponent(query.value)}`)}catch(e){ElMessage.error((e as Error).message)}finally{loading.value=false}}
onMounted(load)
</script>
<template><section class="panel"><div class="panel-title"><h2>历史日志</h2><span class="muted">最近 {{rows.length}} 条</span></div><div class="toolbar"><el-input v-model="query" clearable placeholder="搜索日志内容" style="max-width:420px" @keyup.enter="load" @clear="load"/><el-button type="success" :loading="loading" @click="load">搜索</el-button></div><el-table :data="rows" v-loading="loading" height="620" stripe><el-table-column label="时间" width="190"><template #default="s">{{new Date(s.row.created_at).toLocaleString()}}</template></el-table-column><el-table-column prop="level" label="级别" width="90"/><el-table-column prop="source" label="来源" width="100"/><el-table-column prop="message" label="内容" min-width="400" show-overflow-tooltip/></el-table></section></template>
