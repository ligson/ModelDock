<template>
  <a-card size="small" class="model-card">
    <template #title><span>{{ model.name }}</span></template>
    <template #extra><a-tag :color="model.available ? 'green' : 'red'">{{ model.available ? '可用' : '不可用' }}</a-tag></template>
    <a-space wrap><a-tag>{{ model.format }}</a-tag><a-tag v-if="model.quantization">{{ model.quantization }}</a-tag><a-tag v-if="model.parameters">{{ model.parameters }}</a-tag></a-space>
    <p class="muted path">{{ model.localPath || '来源模型，尚未安装' }}</p>
    <a-typography-text type="secondary">{{ model.managed ? '已纳入管理' : '仅扫描记录' }}</a-typography-text>
    <template #actions><a-button size="small" @click="$emit('test', model)">测试</a-button><a-button v-if="model.managed" size="small" @click="$emit('unmanage', model)">取消管理</a-button><a-button v-else size="small" type="primary" @click="$emit('manage', model)">纳入管理</a-button><a-button size="small" danger @click="$emit('remove', model)">删除</a-button></template>
  </a-card>
</template>
<script setup lang="ts">
import type { ModelManifest } from '../types/api'
defineProps<{ model: ModelManifest }>()
defineEmits<{ test: [model: ModelManifest]; manage: [model: ModelManifest]; unmanage: [model: ModelManifest]; remove: [model: ModelManifest] }>()
</script>
