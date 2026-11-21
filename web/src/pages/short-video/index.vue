<template><div class="page media-workbench"><a-page-header title="短视频工作台" sub-title="快速完成一条可发布的视频" extra=""><template #extra><a-button type="primary" @click="message.info('素材生成任务已加入队列')"><VideoCameraOutlined />生成素材</a-button></template></a-page-header><a-row :gutter="18"><a-col :xs="24" :lg="16"><a-card title="创作输入"><a-textarea v-model:value="brief" :rows="5" placeholder="输入主题、产品卖点或视频脚本…" /><a-space wrap class="workbench-actions"><a-segmented v-model:value="format" :options="['竖屏 9:16','横屏 16:9','方形 1:1']" /><a-select v-model:value="duration" :options="durations" /></a-space></a-card><a-card title="分镜草稿" class="workbench-card"><a-empty v-if="!shots.length" description="还没有分镜，输入创作内容后生成" /><a-list v-else bordered :data-source="shots"><template #renderItem="{item,index}"><a-list-item><a-list-item-meta :title="`镜头 ${index+1} · ${item.title}`" :description="item.detail" /><a-tag>{{ item.status }}</a-tag></a-list-item></template></a-list></a-card></a-col><a-col :xs="24" :lg="8"><a-card title="制作设置"><a-form layout="vertical"><a-form-item label="视觉风格"><a-select v-model:value="style" :options="styles" /></a-form-item><a-form-item label="配音"><a-select v-model:value="voice" :options="voices" /></a-form-item><a-form-item label="字幕"><a-switch v-model:checked="subtitles" /> 自动生成字幕</a-form-item></a-form></a-card><a-card title="制作流程" class="workbench-card"><a-steps direction="vertical" size="small" :current="1"><a-step title="拆解主题" description="生成视频结构和分镜" /><a-step title="生成素材" description="图片、视频、配音和音效" /><a-step title="合成导出" description="字幕、音乐和成片" /></a-steps></a-card></a-col></a-row></div></template>
<script setup lang="ts">
import { ref } from 'vue'
import { message } from 'ant-design-vue'
import { VideoCameraOutlined } from '@ant-design/icons-vue'
type Shot = { title: string; detail: string; status: string }
const brief = ref('')
const format = ref('竖屏 9:16')
const duration = ref('30 秒')
const style = ref('写实')
const voice = ref('自然女声')
const subtitles = ref(true)
const shots = ref<Shot[]>([])
const durations = [{ label: '15 秒', value: '15 秒' }, { label: '30 秒', value: '30 秒' }, { label: '60 秒', value: '60 秒' }]
const styles = [{ label: '写实', value: '写实' }, { label: '电影感', value: '电影感' }, { label: '插画', value: '插画' }]
const voices = [{ label: '自然女声', value: '自然女声' }, { label: '沉稳男声', value: '沉稳男声' }, { label: '暂不配音', value: '暂不配音' }]
</script>
