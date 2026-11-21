<template>
  <div class="page media-workbench">
    <a-page-header
      title="短视频工作台"
      sub-title="快速完成一条可发布的视频"
      extra=""
      ><template #extra
        ><a-button
          type="primary"
          :loading="generating"
          :disabled="!videoModel"
          @click="generateVideo"
          ><VideoCameraOutlined />生成视频素材</a-button
        ></template
      ></a-page-header
    ><a-row :gutter="18"
      ><a-col :xs="24" :lg="16"
        ><a-card title="创作输入"
          ><a-textarea
            v-model:value="brief"
            :rows="5"
            placeholder="输入主题、产品卖点或视频脚本…"
          /><a-space wrap class="workbench-actions"
            ><a-segmented
              v-model:value="format"
              :options="['竖屏 9:16', '横屏 16:9', '方形 1:1']"
            /><a-select
              v-model:value="duration"
              :options="durations"
            /><a-button
              type="primary"
              :loading="generating"
              :disabled="!brief.trim() || !videoModel"
              @click="generateVideo"
              ><VideoCameraOutlined />生成视频素材</a-button
            ></a-space
          ><a-typography-text v-if="!videoModel" type="warning"
            >请先在右侧选择视频生成模型</a-typography-text
          ><a-alert
            v-if="jobError"
            type="error"
            show-icon
            :message="jobError"
            style="margin-top: 16px"
          />
          <div v-if="artifact" class="workbench-result">
            <video
              v-if="artifact.type === 'video'"
              :src="artifact.url"
              controls
            /><a v-if="artifact.url" :href="artifact.url" download
              >下载生成素材</a
            >
          </div></a-card
        ><a-card title="分镜草稿" class="workbench-card"
          ><a-empty
            v-if="!shots.length"
            description="还没有分镜，填写创作内容后点击生成视频素材"
          /><a-list v-else bordered :data-source="shots"
            ><template #renderItem="{ item, index }"
              ><a-list-item
                ><a-list-item-meta
                  :title="`镜头 ${index + 1} · ${item.title}`"
                  :description="item.detail"
                /><a-tag>{{ item.status }}</a-tag></a-list-item
              ></template
            ></a-list
          ></a-card
        ></a-col
      ><a-col :xs="24" :lg="8"
        ><a-card title="模型配置"
          ><a-form layout="vertical"
            ><a-form-item label="脚本/分镜模型"
              ><a-select
                v-model:value="plannerModel"
                :options="modelOptions('文本')"
                placeholder="自动推荐" /></a-form-item
            ><a-form-item label="视频生成模型"
              ><a-select
                v-model:value="videoModel"
                :options="modelOptions('视频')"
                placeholder="选择视频模型" /></a-form-item
            ><a-form-item label="图片参考模型"
              ><a-select
                v-model:value="imageModel"
                :options="modelOptions('图片')"
                placeholder="可选"
                allow-clear /></a-form-item
            ><a-typography-text type="secondary"
              >默认按模型能力推荐，用户选择会保存在当前工作台。</a-typography-text
            ></a-form
          ></a-card
        ><a-card title="制作设置" class="workbench-card"
          ><a-form layout="vertical"
            ><a-form-item label="视觉风格"
              ><a-select v-model:value="style" :options="styles" /></a-form-item
            ><a-form-item label="配音"
              ><a-select v-model:value="voice" :options="voices" /></a-form-item
            ><a-form-item label="字幕"
              ><a-switch v-model:checked="subtitles" />
              自动生成字幕</a-form-item
            ></a-form
          ></a-card
        ><a-card title="制作流程" class="workbench-card"
          ><a-steps
            direction="vertical"
            size="small"
            :current="generating ? 1 : 0"
            ><a-step title="拆解主题" description="生成视频结构和分镜" /><a-step
              title="生成素材"
              description="图片、视频、配音和音效" /><a-step
              title="合成导出"
              description="字幕、音乐和成片" /></a-steps></a-card></a-col
    ></a-row>
  </div>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { message } from "ant-design-vue";
import { VideoCameraOutlined } from "@ant-design/icons-vue";
import { api } from "../../services/api";
import type { ModelManifest } from "../../types/api";
type Shot = { title: string; detail: string; status: string };
const brief = ref("");
const format = ref("竖屏 9:16");
const duration = ref("30 秒");
const style = ref("写实");
const voice = ref("自然女声");
const subtitles = ref(true);
const shots = ref<Shot[]>([]);
const durations = [
  { label: "15 秒", value: "15 秒" },
  { label: "30 秒", value: "30 秒" },
  { label: "60 秒", value: "60 秒" },
];
const styles = [
  { label: "写实", value: "写实" },
  { label: "电影感", value: "电影感" },
  { label: "插画", value: "插画" },
];
const voices = [
  { label: "自然女声", value: "自然女声" },
  { label: "沉稳男声", value: "沉稳男声" },
  { label: "暂不配音", value: "暂不配音" },
];
const models = ref<ModelManifest[]>([]);
const plannerModel = ref("");
const videoModel = ref("");
const imageModel = ref("");
const generating = ref(false);
const jobError = ref("");
const artifact = ref<{ type: string; url: string }>();
let timer: number | undefined;
function modelOptions(capability: string) {
  return models.value
    .filter(
      (item) =>
        item.managed &&
        item.available &&
        (capability === "视频" && /mlx/i.test(item.name)
          ? true
          : capability === "文本"
            ? !(item.task || []).some((t) => /图片|视频|音频/.test(t))
            : (item.task || []).some((t) => t.includes(capability))),
    )
    .map((item) => ({
      label:
        item.name +
        (capability === "视频" && /mlx/i.test(item.name)
          ? " · MLX 视频脚本"
          : ""),
      value: item.id,
    }));
}
async function generateVideo() {
  if (!brief.value.trim() || !videoModel.value) {
    message.warning(
      !brief.value.trim() ? "请先填写创作内容" : "请选择视频生成模型",
    );
    return;
  }
  generating.value = true;
  jobError.value = "";
  artifact.value = undefined;
  try {
    const submitted = await api.generate(videoModel.value, brief.value, "", {
      modality: "video",
      size: format.value.includes("9:16")
        ? "576x1024"
        : format.value.includes("16:9")
          ? "1024x576"
          : "768x768",
    });
    if (submitted.status === "not_ready")
      throw new Error(submitted.message || "视频模型不可用");
    const read = async () => {
      const state = await api.generationJob(submitted.id);
      if (state.status === "completed") {
        artifact.value = { type: "video", url: state.artifactURL || "" };
        generating.value = false;
        window.clearInterval(timer);
        return true;
      }
      if (state.status === "failed")
        throw new Error(state.error || state.message || "生成失败");
      return false;
    };
    if (!(await read()))
      timer = window.setInterval(async () => {
        try {
          if (await read()) window.clearInterval(timer);
        } catch (error) {
          generating.value = false;
          window.clearInterval(timer);
          jobError.value = error instanceof Error ? error.message : "生成失败";
        }
      }, 1000);
  } catch (error) {
    generating.value = false;
    jobError.value =
      error instanceof Error ? error.message : "提交生成任务失败";
  }
}
onMounted(async () => {
  models.value = await api.models();
  const video = modelOptions("视频");
  videoModel.value = video[0]?.value || "";
  const text = modelOptions("文本");
  plannerModel.value = text[0]?.value || "";
});
onUnmounted(() => window.clearInterval(timer));
</script>
