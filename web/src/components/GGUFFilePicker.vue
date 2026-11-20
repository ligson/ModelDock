<template>
  <div class="gguf-picker">
    <a-button v-if="!files.length" size="small" :loading="loading" @click="$emit('inspect')">
      查看仓库文件与推荐量化
    </a-button>
    <template v-else>
      <a-select :value="selected" size="small" style="min-width: 230px" :options="options" @change="$emit('select', $event)" />
      <a-typography-text v-if="selectedFile" type="secondary" class="file-note">
        {{ selectedFile.path }} · {{ selectedFile.recommendation || '可选量化文件' }} · {{ sizeLabel(selectedFile.sizeBytes) }}
      </a-typography-text>
      <a-checkbox v-if="mmproj" :checked="includeMmproj" @change="$emit('include-mmproj', $event.target.checked)">
        同时下载图片输入文件 mmproj（{{ sizeLabel(mmproj.sizeBytes) }}）
      </a-checkbox>
      <a-button size="small" type="primary" :loading="loading" :disabled="!selectedFile || !selectedFile.compatible" @click="$emit('download')">
        下载所选文件
      </a-button>
      <a-typography-text v-if="selectedFile && !selectedFile.compatible" type="danger" class="file-note">
        {{ selectedFile.reason || '当前机器资源不足，暂不推荐此文件' }}
      </a-typography-text>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

export interface GGUFFileOption {
  quantization: string
  path: string
  sizeBytes: number
  recommendation?: string
  compatible: boolean
  reason?: string
}

export interface GGUFProjectionOption {
  path: string
  sizeBytes: number
  quantization: string
}

const props = defineProps<{
  files: GGUFFileOption[]
  mmproj?: GGUFProjectionOption
  selected?: string
  includeMmproj?: boolean
  loading?: boolean
}>()

defineEmits<{
  inspect: []
  select: [value: string]
  'include-mmproj': [value: boolean]
  download: []
}>()

const selectedFile = computed(() => props.files.find(file => file.quantization === props.selected))
const options = computed(() => props.files.map(file => ({
  label: `${file.quantization}${file.recommendation ? ` · ${file.recommendation}` : ''} · ${sizeLabel(file.sizeBytes)}`,
  value: file.quantization,
})))

function sizeLabel(size: number) {
  return size ? `${(size / 1024 ** 3).toFixed(2)} GB` : '大小未知'
}
</script>

<style scoped>
.gguf-picker { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; min-width: 0; }
.file-note { font-size: 12px; line-height: 18px; }
</style>
