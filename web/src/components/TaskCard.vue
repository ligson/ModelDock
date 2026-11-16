<template>
  <a-card size="small"><a-flex justify="space-between"><a-tag :color="task.status === 'installed' ? 'green' : task.status === 'failed' ? 'red' : 'blue'">{{ statusLabel(task.status) }}</a-tag><span>{{ bytes(task.bytesDone) }} / {{ bytes(task.bytesTotal) }}</span></a-flex><a-progress :percent="percent" size="small" :show-info="false" /><p>{{ task.currentFile || task.modelId }}</p><a-typography-text type="secondary" v-if="task.error">{{ task.error }}</a-typography-text><template #actions><a-button v-if="active" size="small" @click="$emit('action','pause')">暂停</a-button><a-button v-if="active" danger size="small" @click="$emit('action','cancel')">取消</a-button><a-button v-if="canResume" size="small" type="primary" @click="$emit('action','resume')">继续</a-button></template></a-card>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import type { Task } from '../types/api'
import { bytes, statusLabel } from '../utils/format'
const props = defineProps<{ task: Task }>()
defineEmits<{ action: [value: string] }>()
const percent = computed(() => props.task.bytesTotal ? Math.min(100, Math.round(props.task.bytesDone / props.task.bytesTotal * 100)) : 0)
const active = computed(() => ['queued','resolving','downloading','verifying','retrying'].includes(props.task.status))
const canResume = computed(() => ['paused','cancelled','failed','stalled'].includes(props.task.status))
</script>
