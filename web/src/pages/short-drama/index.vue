<template><div class="page media-workbench"><a-page-header title="短剧工作台" sub-title="围绕角色、场景和分镜持续制作多集剧情"><template #extra><a-button type="primary" @click="message.info('剧本拆解任务已加入队列')"><ReadOutlined />拆解剧本</a-button></template></a-page-header><a-card title="项目概览"><a-row :gutter="16"><a-col :xs="24" :md="12"><a-input v-model:value="project" addon-before="项目名称" /></a-col><a-col :xs="24" :md="6"><a-input-number v-model:value="episodes" addon-before="集数" :min="1" /></a-col><a-col :xs="24" :md="6"><a-select v-model:value="aspect" :options="aspects" /></a-col></a-row></a-card><a-card title="模型配置" class="workbench-card"><a-row :gutter="16"><a-col :xs="24" :md="8"><a-form-item label="剧本拆解模型"><a-select v-model:value="scriptModel" :options="modelOptions('文本')" placeholder="自动推荐" /></a-form-item></a-col><a-col :xs="24" :md="8"><a-form-item label="镜头视频模型"><a-select v-model:value="videoModel" :options="modelOptions('视频')" placeholder="选择视频模型" /></a-form-item></a-col><a-col :xs="24" :md="8"><a-form-item label="配音模型"><a-select v-model:value="voiceModel" :options="modelOptions('音频')" placeholder="可选" /></a-form-item></a-col></a-row><a-typography-text type="secondary">模型由用户配置，默认值仅来自当前本地已纳入管理的可用模型。</a-typography-text></a-card><a-row :gutter="18" class="workbench-card"><a-col :xs="24" :lg="8"><a-card title="角色卡"><a-empty description="添加主要角色" /><a-button block @click="message.info('角色卡编辑器即将打开')">添加角色</a-button></a-card></a-col><a-col :xs="24" :lg="8"><a-card title="场景卡"><a-empty description="添加固定场景" /><a-button block @click="message.info('场景卡编辑器即将打开')">添加场景</a-button></a-card></a-col><a-col :xs="24" :lg="8"><a-card title="连续性检查"><a-result status="info" title="等待剧本拆解" sub-title="角色外观、服装、场景和镜头关系会在这里检查" /></a-card></a-col></a-row><a-card title="剧集与分镜" class="workbench-card"><a-tabs><a-tab-pane key="1" tab="第 1 集"><a-empty description="输入剧本后生成分镜列表" /></a-tab-pane><a-tab-pane key="2" tab="第 2 集"><a-empty description="暂无内容" /></a-tab-pane></a-tabs></a-card></div></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message } from 'ant-design-vue'
import { ReadOutlined } from '@ant-design/icons-vue'
import { api } from '../../services/api'
import type { ModelManifest } from '../../types/api'
const project = ref('未命名短剧')
const episodes = ref(3)
const aspect = ref('竖屏 9:16')
const aspects = [{ label: '竖屏 9:16', value: '竖屏 9:16' }, { label: '横屏 16:9', value: '横屏 16:9' }]
const models = ref<ModelManifest[]>([]); const scriptModel = ref(''); const videoModel = ref(''); const voiceModel = ref('')
function modelOptions(capability: string) { return models.value.filter(item => item.managed && item.available && (item.task || []).some(task => task.includes(capability))).map(item => ({ label: item.name, value: item.id })) }
onMounted(async () => { models.value = await api.models(); scriptModel.value = modelOptions('文本')[0]?.value || ''; videoModel.value = modelOptions('视频')[0]?.value || ''; voiceModel.value = modelOptions('音频')[0]?.value || '' })
</script>
