<template>
  <div class="page ollama-page">
    <a-page-header title="Ollama" sub-title="本地推理运行时与已安装模型" />
    <a-card title="连接设置" class="ollama-card">
      <a-space direction="vertical" size="middle" style="width: 100%">
        <a-input v-model:value="baseURL" addon-before="服务地址" placeholder="http://127.0.0.1:11434" />
        <a-space>
          <a-button type="primary" :loading="loading" @click="save">保存并测试</a-button>
          <a-tag v-if="status" :color="status.connected ? 'green' : 'red'">{{ status.connected ? '已连接' : '未连接' }}</a-tag>
        </a-space>
        <a-alert v-if="status?.error" type="error" :message="status.error" show-icon />
        <a-typography-text type="secondary">ModelDock 只保存地址，不接管 Ollama 的模型文件。远程地址需要允许本机访问。</a-typography-text>
      </a-space>
    </a-card>
    <a-card title="已安装模型" class="ollama-card">
      <template #extra><a-button @click="load">刷新</a-button></template>
      <a-list v-if="status?.models.length" :data-source="status.models" bordered>
        <template #renderItem="{ item }"><a-list-item><a-list-item-meta :title="item.name" :description="item.tags?.[0] || 'Ollama 本地模型'" /><span>{{ formatBytes(item.downloads) }}</span><a-button size="small" type="primary" @click="importModel(item.name)">纳入交互</a-button></a-list-item></template>
      </a-list>
      <a-empty v-else description="没有发现 Ollama 模型" />
    </a-card>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { api } from '../../services/api'
import type { OllamaStatus } from '../../types/api'
const baseURL = ref('http://127.0.0.1:11434')
const status = ref<OllamaStatus>()
const loading = ref(false)
function formatBytes(value?: number) { if (!value) return '大小未知'; if (value > 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`; return `${(value / 1024 ** 2).toFixed(1)} MB` }
async function load() { try { status.value = await api.ollama(); baseURL.value = status.value.baseURL } catch (error) { message.error(error instanceof Error ? error.message : '读取 Ollama 配置失败') } }
async function save() { loading.value = true; try { status.value = await api.saveOllama(baseURL.value); status.value.connected ? message.success('Ollama 已连接') : message.error(status.value.error || 'Ollama 连接失败') } finally { loading.value = false } }
async function importModel(name: string) { try { await api.importOllama(name); message.success(`${name} 已加入模型交互`) } catch (error) { message.error(error instanceof Error ? error.message : '纳入模型失败') } }
onMounted(load)
</script>
