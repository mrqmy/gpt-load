<script setup lang="ts">
import type { Group } from "@/types/models";
import { getGroupDisplayName } from "@/utils/display";
import { Add, LinkOutline, Search } from "@vicons/ionicons5";
import { NButton, NCard, NEmpty, NInput, NSpin, NTag } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import AggregateGroupModal from "./AggregateGroupModal.vue";
import GroupFormModal from "./GroupFormModal.vue";

const { t } = useI18n();

interface Props {
  groups: Group[];
  selectedGroup: Group | null;
  loading?: boolean;
}

interface Emits {
  (e: "group-select", group: Group): void;
  (e: "refresh"): void;
  (e: "refresh-and-select", groupId: number): void;
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
});

const emit = defineEmits<Emits>();

const searchText = ref("");
const showGroupModal = ref(false);
// 存储分组项 DOM 元素的引用
const groupItemRefs = ref(new Map());
const showAggregateGroupModal = ref(false);
// 跟踪哪些聚合分组是展开的
const expandedGroups = ref<Set<number>>(new Set());

// 获取所有作为子分组的组ID
const subGroupIds = computed(() => {
  const ids = new Set<number>();
  props.groups.forEach(group => {
    if (group.group_type === "aggregate" && group.sub_group_ids) {
      group.sub_group_ids.forEach(id => ids.add(id));
    }
  });
  return ids;
});

// 构建分组ID到分组对象的映射
const groupMap = computed(() => {
  const map = new Map<number, Group>();
  props.groups.forEach(group => {
    if (group.id) map.set(group.id, group);
  });
  return map;
});

// 构建树形结构：过滤掉子分组，只在根级别显示独立分组和聚合分组
const groupTree = computed(() => {
  const search = searchText.value.toLowerCase().trim();
  
  // 过滤函数
  const matchesSearch = (group: Group) => {
    if (!search) return true;
    return (
      group.name.toLowerCase().includes(search) ||
      group.display_name?.toLowerCase().includes(search)
    );
  };

  // 如果有搜索词，显示所有匹配的组（平铺显示便于搜索）
  if (search) {
    return props.groups.filter(matchesSearch).map(group => ({
      ...group,
      isSubGroup: subGroupIds.value.has(group.id!),
      children: [] as (Group & { weight?: number })[],
    }));
  }

  // 无搜索时，构建树形结构
  return props.groups
    .filter(group => !subGroupIds.value.has(group.id!)) // 过滤掉子分组
    .map(group => {
      // 根据 sub_group_ids 构建子分组列表
      let children: (Group & { weight?: number })[] = [];
      if (group.group_type === "aggregate" && group.sub_group_ids && group.sub_group_ids.length > 0) {
        children = group.sub_group_ids
          .map(id => groupMap.value.get(id))
          .filter((g): g is Group => g !== undefined)
          .map(g => ({ ...g, weight: 0 })); // weight 暂时设为 0，后续可从 sub_groups 获取
        
        // 如果有 sub_groups 数据，获取权重信息
        if (group.sub_groups && group.sub_groups.length > 0) {
          const weightMap = new Map<number, number>();
          group.sub_groups.forEach(sg => {
            if (sg.group?.id) weightMap.set(sg.group.id, sg.weight);
          });
          children = children.map(c => ({
            ...c,
            weight: c.id ? (weightMap.get(c.id) ?? 0) : 0,
          }));
        }
      }
      
      return {
        ...group,
        isSubGroup: false,
        children,
      };
    });
});

// 切换聚合分组的展开/折叠状态
function toggleExpand(groupId: number) {
  if (expandedGroups.value.has(groupId)) {
    expandedGroups.value.delete(groupId);
  } else {
    expandedGroups.value.add(groupId);
  }
}

// 检查聚合分组是否展开
function isExpanded(groupId: number) {
  return expandedGroups.value.has(groupId);
}



// 监听选中项 ID 的变化，并自动滚动到该项
watch(
  () => props.selectedGroup?.id,
  id => {
    if (!id || props.groups.length === 0) {
      return;
    }

    const element = groupItemRefs.value.get(id);
    if (element) {
      element.scrollIntoView({
        behavior: "smooth", // 平滑滚动
        block: "nearest", // 将元素滚动到最近的边缘
      });
    }
  },
  {
    flush: "post", // 确保在 DOM 更新后执行回调
    immediate: true, // 立即执行一次以处理初始加载
  }
);

function handleGroupClick(group: Group & { children?: unknown[] }) {
  emit("group-select", group);
  // 点击聚合分组时自动展开/折叠
  if (group.group_type === "aggregate" && group.id) {
    const hasChildren = group.children && group.children.length > 0;
    const hasSubGroupIds = group.sub_group_ids && group.sub_group_ids.length > 0;
    if (hasChildren || hasSubGroupIds) {
      toggleExpand(group.id);
    }
  }
}

// 获取渠道类型的标签颜色
function getChannelTagType(channelType: string) {
  switch (channelType) {
    case "openai":
      return "success";
    case "gemini":
      return "info";
    case "anthropic":
      return "warning";
    default:
      return "default";
  }
}

function openCreateGroupModal() {
  showGroupModal.value = true;
}

function openCreateAggregateGroupModal() {
  showAggregateGroupModal.value = true;
}

function handleGroupCreated(group: Group) {
  showGroupModal.value = false;
  showAggregateGroupModal.value = false;
  if (group?.id) {
    emit("refresh-and-select", group.id);
  }
}
</script>

<template>
  <div class="group-list-container">
    <n-card class="group-list-card modern-card" :bordered="false" size="small">
      <!-- 搜索框 -->
      <div class="search-section">
        <n-input
          v-model:value="searchText"
          :placeholder="t('keys.searchGroupPlaceholder')"
          size="small"
          clearable
        >
          <template #prefix>
            <n-icon :component="Search" />
          </template>
        </n-input>
      </div>

      <!-- 分组列表 -->
      <div class="groups-section">
        <n-spin :show="loading" size="small">
          <div v-if="groupTree.length === 0 && !loading" class="empty-container">
            <n-empty
              size="small"
              :description="searchText ? t('keys.noMatchingGroups') : t('keys.noGroups')"
            />
          </div>
          <div v-else class="groups-list">
            <template v-for="group in groupTree" :key="group.id">
              <!-- 主分组项 -->
              <div
                class="group-item"
                :class="{
                  active: selectedGroup?.id === group.id,
                  aggregate: group.group_type === 'aggregate',
                  'has-children': group.children && group.children.length > 0,
                  'is-sub-group': group.isSubGroup,
                }"
                @click="handleGroupClick(group)"
                :ref="el => { if (el) groupItemRefs.set(group.id, el); }"
              >
                <!-- 展开/折叠按钮 -->
                <div
                  v-if="group.group_type === 'aggregate' && group.children && group.children.length > 0"
                  class="expand-btn"
                  @click.stop="toggleExpand(group.id!)"
                >
                  <span :class="{ rotated: isExpanded(group.id!) }">▶</span>
                </div>
                <div v-else class="expand-placeholder"></div>
                
                <div class="group-icon">
                  <span v-if="group.group_type === 'aggregate'">🔗</span>
                  <span v-else-if="group.channel_type === 'openai'">🤖</span>
                  <span v-else-if="group.channel_type === 'gemini'">💎</span>
                  <span v-else-if="group.channel_type === 'anthropic'">🧠</span>
                  <span v-else>🔧</span>
                </div>
                <div class="group-content">
                  <div class="group-name">{{ getGroupDisplayName(group) }}</div>
                  <div class="group-meta">
                    <n-tag size="tiny" :type="getChannelTagType(group.channel_type)">
                      {{ group.channel_type }}
                    </n-tag>
                    <n-tag v-if="group.group_type === 'aggregate'" size="tiny" type="warning" round>
                      {{ t("keys.aggregateGroup") }}
                    </n-tag>
                    <span v-if="group.isSubGroup" class="sub-group-badge">
                      ↳ {{ t("keys.subGroup") }}
                    </span>
                    <span v-else-if="group.group_type !== 'aggregate'" class="group-id">
                      #{{ group.name }}
                    </span>
                  </div>
                </div>
              </div>
              
              <!-- 子分组列表 -->
              <div
                v-if="group.children && group.children.length > 0 && isExpanded(group.id!)"
                class="sub-groups"
              >
                <div
                  v-for="child in group.children"
                  :key="child.id"
                  class="group-item sub-group-item"
                  :class="{ active: selectedGroup?.id === child.id }"
                  @click="handleGroupClick(child)"
                  :ref="el => { if (el) groupItemRefs.set(child.id, el); }"
                >
                  <div class="tree-line"></div>
                  <div class="group-icon small">
                    <span v-if="child.channel_type === 'openai'">🤖</span>
                    <span v-else-if="child.channel_type === 'gemini'">💎</span>
                    <span v-else-if="child.channel_type === 'anthropic'">🧠</span>
                    <span v-else>🔧</span>
                  </div>
                  <div class="group-content">
                    <div class="group-name">{{ getGroupDisplayName(child) }}</div>
                    <div class="group-meta">
                      <n-tag size="tiny" :type="getChannelTagType(child.channel_type)">
                        {{ child.channel_type }}
                      </n-tag>
                      <span class="weight-badge">{{ child.weight }}%</span>
                    </div>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </n-spin>
      </div>

      <!-- 添加分组按钮 -->
      <div class="add-section">
        <n-button type="success" size="small" block @click="openCreateGroupModal">
          <template #icon>
            <n-icon :component="Add" />
          </template>
          {{ t("keys.createGroup") }}
        </n-button>
        <n-button type="info" size="small" block @click="openCreateAggregateGroupModal">
          <template #icon>
            <n-icon :component="LinkOutline" />
          </template>
          {{ t("keys.createAggregateGroup") }}
        </n-button>
      </div>
    </n-card>
    <group-form-modal v-model:show="showGroupModal" @success="handleGroupCreated" />
    <aggregate-group-modal
      v-model:show="showAggregateGroupModal"
      :groups="groups"
      @success="handleGroupCreated"
    />
  </div>
</template>

<style scoped>
:deep(.n-card__content) {
  height: 100%;
}

.groups-section::-webkit-scrollbar {
  width: 1px;
  height: 1px;
}

.group-list-container {
  height: 100%;
}

.group-list-card {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--card-bg-solid);
}

.group-list-card:hover {
  transform: none;
  box-shadow: var(--shadow-lg);
}

.search-section {
  height: 41px;
}

.groups-section {
  flex: 1;
  height: calc(100% - 120px);
  overflow: auto;
}

.empty-container {
  padding: 20px 0;
}

.groups-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 100%;
  overflow-y: auto;
  width: 100%;
}

.group-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.2s ease;
  border: 1px solid var(--border-color);
  font-size: 12px;
  color: var(--text-primary);
  background: transparent;
  box-sizing: border-box;
  position: relative;
}

/* 展开/折叠按钮 */
.expand-btn {
  width: 16px;
  height: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 10px;
  color: var(--text-secondary);
  cursor: pointer;
  transition: transform 0.2s ease;
  flex-shrink: 0;
}

.expand-btn:hover {
  color: var(--primary-color);
}

.expand-btn span {
  display: inline-block;
  transition: transform 0.2s ease;
}

.expand-btn span.rotated {
  transform: rotate(90deg);
}

.expand-placeholder {
  width: 16px;
  flex-shrink: 0;
}

/* 子分组容器 */
.sub-groups {
  margin-left: 8px;
  padding-left: 8px;
  border-left: 1px dashed var(--border-color);
}

/* 子分组项 */
.sub-group-item {
  padding: 6px 8px;
  margin-left: 8px;
}

.sub-group-item .group-icon.small {
  width: 22px;
  height: 22px;
  font-size: 12px;
}

.tree-line {
  display: none;
}

/* 权重徽章 */
.weight-badge {
  font-size: 10px;
  padding: 1px 6px;
  background: var(--primary-color);
  color: white;
  border-radius: 10px;
  font-weight: 500;
}

/* 子分组标识 */
.sub-group-badge {
  font-size: 10px;
  color: var(--text-secondary);
  opacity: 0.8;
}

.group-item.is-sub-group {
  opacity: 0.7;
  margin-left: 16px;
  border-style: dotted;
}

/* 聚合分组样式 */
.group-item.aggregate {
  border-style: dashed;
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.02) 0%, rgba(102, 126, 234, 0.05) 100%);
}

:root.dark .group-item.aggregate {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.05) 0%, rgba(102, 126, 234, 0.1) 100%);
  border-color: rgba(102, 126, 234, 0.2);
}

.group-item:hover,
.group-item.aggregate:hover {
  background: var(--bg-tertiary);
  border-color: var(--primary-color);
}

.group-item.aggregate:hover {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.05) 0%, rgba(102, 126, 234, 0.1) 100%);
  border-style: dashed;
}

:root.dark .group-item:hover {
  background: rgba(102, 126, 234, 0.1);
  border-color: rgba(102, 126, 234, 0.3);
}

:root.dark .group-item.aggregate:hover {
  background: linear-gradient(135deg, rgba(102, 126, 234, 0.1) 0%, rgba(102, 126, 234, 0.15) 100%);
  border-color: rgba(102, 126, 234, 0.4);
}

.group-item.aggregate.active {
  background: var(--primary-gradient);
  border-style: solid;
}

.group-item.active,
:root.dark .group-item.active,
:root.dark .group-item.aggregate.active {
  background: var(--primary-gradient);
  color: white;
  border-color: transparent;
  box-shadow: var(--shadow-md);
  border-style: solid;
}

.group-icon {
  font-size: 16px;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-secondary);
  border-radius: 6px;
  flex-shrink: 0;
  box-sizing: border-box;
}

.group-item.active .group-icon {
  background: rgba(255, 255, 255, 0.2);
}

.group-content {
  flex: 1;
  min-width: 0;
}

.group-name {
  font-weight: 600;
  font-size: 14px;
  line-height: 1.2;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  flex-wrap: wrap;
}

.group-id {
  opacity: 0.8;
  color: var(--text-secondary);
}

.group-item.active .group-id {
  opacity: 0.9;
  color: white;
}

.add-section {
  border-top: 1px solid var(--border-color);
  padding-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 滚动条样式 */
.groups-list::-webkit-scrollbar {
  width: 4px;
}

.groups-list::-webkit-scrollbar-track {
  background: transparent;
}

.groups-list::-webkit-scrollbar-thumb {
  background: var(--scrollbar-bg);
  border-radius: 2px;
}

.groups-list::-webkit-scrollbar-thumb:hover {
  background: var(--border-color);
}

/* 暗黑模式特殊样式 */
:root.dark .group-item {
  border-color: rgba(255, 255, 255, 0.05);
}

:root.dark .group-icon {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

:root.dark .search-section :deep(.n-input) {
  --n-border: 1px solid rgba(255, 255, 255, 0.08);
  --n-border-hover: 1px solid rgba(102, 126, 234, 0.4);
  --n-border-focus: 1px solid var(--primary-color);
  background: rgba(255, 255, 255, 0.03);
}

/* 标签样式优化 */
:root.dark .group-meta :deep(.n-tag) {
  background: rgba(102, 126, 234, 0.15);
  border: 1px solid rgba(102, 126, 234, 0.3);
}

:root.dark .group-item.active .group-meta :deep(.n-tag) {
  background: rgba(255, 255, 255, 0.2);
  border-color: rgba(255, 255, 255, 0.3);
}
</style>
