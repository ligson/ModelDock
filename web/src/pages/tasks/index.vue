<template><div class="page"><a-page-header title="下载任务" extra=""><template #extra><a-button @click="load">刷新</a-button></template></a-page-header><a-row :gutter="16"><a-col :xs="24" :lg="8" v-for="task in tasks" :key="task.id"><TaskCard :task="task" @action="action(task.id,$event)" /></a-col></a-row><a-empty v-if="!tasks.length" description="暂无下载任务" /></div></template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'; import { message } from 'ant-design-vue'; import TaskCard from '../../components/TaskCard.vue'; import { api } from '../../services/api'; import type { Task } from '../../types/api'
const tasks=ref<Task[]>([]); let timer:number|undefined
async function load(){tasks.value=await api.tasks()}; async function action(id:string,value:string){await api.taskAction(id,value);message.success(value==='resume'?'已继续任务':value==='pause'?'已暂停任务':'已取消任务');await load()}
onMounted(async()=>{await load();timer=window.setInterval(load,2500)});onUnmounted(()=>window.clearInterval(timer))
</script>
