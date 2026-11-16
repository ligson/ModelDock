<template><div class="page"><a-page-header title="本地模型" sub-title="数据库索引中的模型记录" extra=""><template #extra><a-button type="primary" :loading="scanning" @click="scan">扫描模型目录</a-button></template></a-page-header><a-alert v-if="notice" :message="notice" type="info" show-icon closable @close="notice=''" /><a-alert v-if="ollamaModels.length" message="Ollama 模型也已纳入统一模型索引，可在模型交互中直接使用。" type="info" show-icon /><a-row :gutter="16"><a-col :xs="24" :lg="8" v-for="model in managedModels" :key="model.id"><ModelCard :model="model" @test="test" @manage="manage" @unmanage="unmanage" @remove="remove" /></a-col></a-row><a-empty v-if="!managedModels.length" description="还没有本地模型记录" /></div></template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { message, Modal } from 'ant-design-vue'
import ModelCard from '../../components/ModelCard.vue'; import { api } from '../../services/api'; import type { ModelManifest } from '../../types/api'
const models=ref<ModelManifest[]>([]); const scanning=ref(false); const notice=ref(''); const managedModels = computed(() => models.value.filter(item => item.source === 'local' || item.source === 'ollama')); const ollamaModels = computed(() => models.value.filter(item => item.source === 'ollama'))
async function load(){models.value=await api.models()}; async function scan(){scanning.value=true;try{await api.scan();await load();message.success('扫描完成')}finally{scanning.value=false}}
async function test(m:ModelManifest){const result=await api.testModel(m.id);message[result.status==='available'?'success':'error'](result.detail)}
async function manage(m:ModelManifest){await api.manage(m);await load();message.success('已纳入管理')}; async function unmanage(m:ModelManifest){await api.unmanage(m.id);await load();message.success('已取消管理')}
function remove(m:ModelManifest){Modal.confirm({title:'删除模型',content:m.managed?'是否同时删除磁盘文件？此操作无法撤销。':'只从索引中移除，保留磁盘文件。',okText:'继续',cancelText:'取消',async onOk(){await api.deleteModel(m.id,m.managed);await load();message.success('处理完成')}})}
onMounted(load)
</script>
