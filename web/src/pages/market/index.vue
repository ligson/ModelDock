<template><div class="page"><a-card><a-space direction="vertical" style="width:100%" size="large"><a-space><a-select v-model:value="source" style="width:180px" :options="sources" /><a-input-search v-model:value="query" placeholder="搜索公开模型" enter-button="搜索" @search="search" /></a-space><a-alert v-if="notice" :message="notice" type="info" show-icon /><a-row :gutter="16"><a-col :xs="24" :md="12" :lg="8" v-for="item in results" :key="item.id"><a-card size="small" :title="item.name"><a-typography-text type="secondary">{{ item.source }}</a-typography-text><template #actions><a-button type="link" @click="importModel(item)">加入模型库</a-button></template></a-card></a-col></a-row><a-empty v-if="searched && !results.length" description="没有找到模型" /></a-space></a-card></div></template>
<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../services/api'
const source = ref('huggingface'); const query = ref(''); const searched = ref(false); const notice = ref(''); const results = ref<Array<{id:string;name:string;source:string;downloads?:number}>>([])
const sources = [{value:'huggingface',label:'Hugging Face'},{value:'modelscope',label:'ModelScope'},{value:'ollama',label:'本机 Ollama'}]
async function search(){ if(!query.value.trim())return; results.value=await api.search(source.value,query.value); searched.value=true }
async function importModel(item: {id:string;name:string;source:string}) { const manifest=await api.manifest(item.source,item.id); notice.value=`已加入：${manifest.name}`; message.success('模型清单已加入模型库') }
</script>
