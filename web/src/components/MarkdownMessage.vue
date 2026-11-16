<template>
  <div class="markdown-message">
    <div class="markdown-toolbar">
      <a-button type="text" size="small" :icon="h(CopyOutlined)" @click="copySource">复制源码</a-button>
    </div>
    <div class="markdown-body" v-html="html" @click="handleClick" />
  </div>
</template>

<script setup lang="ts">
import { computed, h } from 'vue'
import { message } from 'ant-design-vue'
import { CopyOutlined } from '@ant-design/icons-vue'
import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const props = defineProps<{ content: string }>()
const parser = new MarkdownIt({ html: false, breaks: true, linkify: true })
const html = computed(() => DOMPurify.sanitize(parser.render(props.content), { USE_PROFILES: { html: true } }))

async function copySource() {
  await navigator.clipboard.writeText(props.content)
  message.success('Markdown 源码已复制')
}

async function handleClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  const code = target.closest('pre')?.querySelector('code')
  if (!code || !(target.closest('pre') as HTMLElement)?.contains(target)) return
  await navigator.clipboard.writeText(code.textContent || '')
  message.success('代码已复制')
}
</script>
