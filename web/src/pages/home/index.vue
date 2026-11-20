<template>
  <div class="page">
    <a-alert v-if="loadError" type="warning" show-icon :message="loadError" class="home-error">
      <template #action><a-button size="small" @click="load">重试</a-button></template>
    </a-alert>
    <a-spin :spinning="loading && !loaded">
      <a-row :gutter="16"><a-col v-for="item in metrics" :key="item.label" :xs="24" :md="8"><a-card size="small"><a-statistic :title="item.label" :value="item.value" /><a-typography-text type="secondary">{{ item.detail }}</a-typography-text></a-card></a-col></a-row>
      <a-divider orientation="left">硬件推荐</a-divider>
      <a-row :gutter="16"><a-col v-for="item in recommendations.slice(0,6)" :key="item.model.id" :xs="24" :lg="8"><a-card size="small"><template #title><span>{{ item.model.name }}</span><a v-if="sourceURL(item.model.source,item.model.id)" class="model-source-link" :href="sourceURL(item.model.source,item.model.id)" target="_blank" rel="noreferrer" title="打开模型社区来源"><LinkOutlined /></a></template><a-card-meta :description="item.reason" /><a-tag class="card-tag">{{ item.status }}</a-tag></a-card></a-col><a-empty v-if="loaded && !recommendations.length" description="导入模型后显示推荐" /></a-row>
      <a-divider orientation="left">热门模型</a-divider>
      <a-row :gutter="16"><a-col v-for="item in catalog" :key="item.id" :xs="24" :md="8"><a-card size="small"><template #title><span>{{ item.name }}</span><a v-if="sourceURL(item.source,item.id)" class="model-source-link" :href="sourceURL(item.source,item.id)" target="_blank" rel="noreferrer" title="打开模型社区来源"><LinkOutlined /></a></template><a-tag>{{ item.category }}</a-tag><p>{{ item.description }}</p></a-card></a-col></a-row>
    </a-spin>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../../services/api'
import { LinkOutlined } from '@ant-design/icons-vue'
import { sourceURL } from '../../utils/source'
import type { CatalogItem, Host, Recommendation, RuntimeEngine } from '../../types/api'
const host = ref<Host>({ os:'', arch:'', capabilities:[], runtimes:[] }); const runtimes = ref<RuntimeEngine[]>([]); const recommendations = ref<Recommendation[]>([]); const catalog = ref<CatalogItem[]>([]); const loading = ref(false); const loaded = ref(false); const loadError = ref('')
const metrics = computed(() => [{ label:'系统', value:`${host.value.os} · ${host.value.arch}`, detail:host.value.cpu || '通用处理器' }, { label:'内存', value:`${host.value.memoryGB || '未知'} GB`, detail:host.value.gpus?.length ? host.value.gpus.map(gpu => `${gpu.name}${gpu.memoryGB ? ` · ${gpu.memoryGB} GB` : ''}`).join('、') : (host.value.capabilities.join('、') || '未发现显卡') }, { label:'可用运行时', value:runtimes.value.filter(item => item.installed).length, detail:runtimes.value.filter(item => item.installed).map(item => `${item.name}${item.version ? ` ${item.version}` : ''}`).join('、') || '暂未发现' }])
async function load() {
  if (loading.value) return
  loading.value = true
  loadError.value = ''
  const results = await Promise.allSettled([api.host(), api.recommend(), api.catalog(), api.runtimeEngines()])
  const errors: string[] = []
  if (results[0].status === 'fulfilled') host.value = results[0].value; else errors.push('硬件信息')
  if (results[1].status === 'fulfilled') recommendations.value = results[1].value; else errors.push('硬件推荐')
  if (results[2].status === 'fulfilled') catalog.value = results[2].value; else errors.push('热门模型')
  if (results[3].status === 'fulfilled') runtimes.value = results[3].value.engines; else errors.push('运行时信息')
  loaded.value = true
  if (errors.length) loadError.value = `${errors.join('、')}加载失败，可重试`
  loading.value = false
}
onMounted(load)
</script>
<style scoped>
.home-error { margin-bottom: 16px; }
</style>
