<template>
  <div class="page">
    <a-card title="模型存储目录">
      <a-space direction="vertical" style="width:100%">
        <a-space><a-input v-model:value="path" placeholder="目录路径" /><a-input v-model:value="name" placeholder="目录名称" /><a-button type="primary" @click="add">添加目录</a-button></a-space>
        <a-list :data-source="roots" bordered><template #renderItem="{item}"><a-list-item><a-list-item-meta :title="item.name" :description="item.path"><template #avatar><a-badge :status="item.available?'success':'error'" /></template></a-list-item-meta><span>{{ item.modelCount||0 }} 个模型 · {{ item.available ? `剩余 ${bytes(item.freeBytes)} / ${bytes(item.totalBytes)}` : '离线' }}</span><a-space><a-tag v-if="item.default" color="blue">默认</a-tag><a-button v-else danger size="small" @click="remove(item.id)">移除</a-button></a-space></a-list-item></template></a-list>
        <a-divider orientation="left">迁移模型</a-divider><a-space><a-select v-model:value="from" style="min-width:240px" :options="roots.filter(item=>item.available).map(item=>({label:item.name,value:item.path}))" /><a-input v-model:value="to" placeholder="目标目录" /><a-button @click="migrate">迁移并校验</a-button></a-space><a-typography-text type="secondary">迁移会复制并校验模型，来源文件会保留。</a-typography-text>
      </a-space>
    </a-card>
    <a-card title="运行时目录" style="margin-top:16px">
      <a-space direction="vertical" style="width:100%"><a-input v-model:value="runtimeRoot" placeholder="ModelDock 运行时目录" /><a-button type="primary" :loading="runtimeSaving" @click="saveRuntimeRoot">保存运行时目录</a-button><a-typography-text type="secondary">模型目录只保存权重；MLX、llama.cpp 等运行组件和脚本会放在这里。换机器后只需重新准备运行时，不必修改模型路径。</a-typography-text><a-list v-if="runtimeInfo" size="small" bordered :data-source="runtimeInfo.runtimes"><template #renderItem="{item}"><a-list-item><span>{{ item.name }}</span><a-tag :color="item.installed?'green':'default'">{{ item.installed?'已准备':'未准备' }}</a-tag></a-list-item></template></a-list></a-space>
    </a-card>
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { message, Modal } from 'ant-design-vue'
import { api } from '../../services/api'
import { bytes } from '../../utils/format'
import type { RuntimeDirectory, StorageRoot } from '../../types/api'
const roots=ref<StorageRoot[]>([]); const path=ref(''); const name=ref(''); const from=ref(''); const to=ref(''); const runtimeRoot=ref(''); const runtimeInfo=ref<RuntimeDirectory>(); const runtimeSaving=ref(false)
async function load(){roots.value=(await api.storage()).roots;if(!from.value)from.value=roots.value.find(item=>item.available)?.path||'';runtimeInfo.value=await api.runtime();runtimeRoot.value=runtimeInfo.value.root}
async function add(){await api.addRoot(path.value,name.value);path.value='';name.value='';await load();message.success('目录已添加')}
function remove(id:string){Modal.confirm({title:'移除目录记录',content:'只移除 ModelDock 配置，不删除目录和模型文件。',async onOk(){await api.removeRoot(id);await load()}})}
async function migrate(){if(!from.value||!to.value)return;await api.migrate(from.value,to.value);await load();message.success('迁移完成')}
async function saveRuntimeRoot(){runtimeSaving.value=true;try{runtimeInfo.value=await api.saveRuntime(runtimeRoot.value);runtimeRoot.value=runtimeInfo.value.root;message.success('运行时目录已保存')}catch(error){message.error(error instanceof Error?error.message:'保存运行时目录失败')}finally{runtimeSaving.value=false}}
onMounted(load)
</script>
