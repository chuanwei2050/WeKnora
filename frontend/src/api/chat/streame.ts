import { fetchEventSource } from '@microsoft/fetch-event-source';
import { ref, onUnmounted } from 'vue';
import { generateRandomString } from '@/utils/index';
import { createIdempotencyKey } from '@/utils/idempotency-key';
import i18n from '@/i18n';
import { getApiBaseUrl } from '@/utils/api-base';
import { getEmbeddedCSRFToken, getEmbeddedSessionToken, getRuntimeMode } from '@/utils/embedded-runtime';
import { buildIntegrationMessageBody, isTerminalStreamResponse, mapIntegrationEvent, shouldReportUnexpectedStreamClose, type StreamRequestParams } from './integration-stream';



interface StreamOptions {
  // 请求方法 (默认POST)
  method?: 'GET' | 'POST'
  // 请求头
  headers?: Record<string, string>
  // 请求体自动序列化
  body?: Record<string, any>
  // 流式渲染间隔 (ms)
  chunkInterval?: number
}

export function useStream() {
  // 响应式状态
  const output = ref('')              // 显示内容
  const isStreaming = ref(false)      // 流状态
  const isLoading = ref(false)        // 初始加载
  const error = ref<string | null>(null)// 错误信息
  let controller = new AbortController()

  // 流式渲染缓冲
  let buffer: string[] = []
  let renderTimer: number | null = null

  // 启动流式请求
  const startStream = async (params: StreamRequestParams) => {
    if (isStreaming.value) return;

    // 重置状态
    output.value = '';
    error.value = null;
    isStreaming.value = true;
    isLoading.value = true;

    // 获取API配置
    const apiUrl = getApiBaseUrl();
    
    // 获取JWT Token
    const isIntegrationWidget = getRuntimeMode() === 'embedded-widget';
    const idempotencyKey = createIdempotencyKey();
    const activeController = controller;
    let receivedTerminalEvent = false;
    let lastEventId = '';
    const seenEventIds = new Set<string>();
    const token = isIntegrationWidget ? getEmbeddedSessionToken() : localStorage.getItem('weknora_token');
    if (!token) {
      error.value = i18n.global.t('error.tokenNotFound');
      stopStream();
      return;
    }

    // 获取跨租户访问请求头
    const selectedTenantId = localStorage.getItem('weknora_selected_tenant_id');
    const defaultTenantId = localStorage.getItem('weknora_tenant');
    let tenantIdHeader: string | null = null;
    if (!isIntegrationWidget && selectedTenantId) {
      try {
        const defaultTenant = defaultTenantId ? JSON.parse(defaultTenantId) : null;
        const defaultId = defaultTenant?.id ? String(defaultTenant.id) : null;
        if (selectedTenantId !== defaultId) {
          tenantIdHeader = selectedTenantId;
        }
      } catch (e) {
        console.error('Failed to parse tenant info', e);
      }
    }

    // Validate knowledge_base_ids for agent-chat requests
    // Note: knowledge_base_ids can be empty if user hasn't selected any, but we allow it
    // The backend will handle the case when no knowledge bases are selected
    const isAgentChat = params.url === '/api/v1/agent-chat';
    // Removed validation - allow empty knowledge_base_ids array
    // The backend should handle this case appropriately

    try {
      let url = isIntegrationWidget
        ? `${apiUrl}/api/integration/v1/chat/sessions/${params.session_id}/messages`
        : params.method == "POST"
          ? `${apiUrl}${params.url}/${params.session_id}`
          : `${apiUrl}${params.url}/${params.session_id}?message_id=${params.query}`;
      
      // Prepare POST body with required fields for agent-chat
      // knowledge_base_ids array and agent_enabled can update Session's SessionAgentConfig
      const postBody: any = isIntegrationWidget ? buildIntegrationMessageBody(params) : {
        query: params.query,
        agent_enabled: params.agent_enabled !== undefined ? params.agent_enabled : true
      };
      if (!isIntegrationWidget) {
        // Always include knowledge_base_ids for agent-chat (already validated above)
        if (params.knowledge_base_ids !== undefined && params.knowledge_base_ids.length > 0) {
          postBody.knowledge_base_ids = params.knowledge_base_ids;
        }
        if (params.knowledge_ids !== undefined && params.knowledge_ids.length > 0) postBody.knowledge_ids = params.knowledge_ids;
        if (params.agent_id) postBody.agent_id = params.agent_id;
        if (params.web_search_enabled !== undefined) postBody.web_search_enabled = params.web_search_enabled;
        if (params.filter_disabled_folders !== undefined) postBody.filter_disabled_folders = params.filter_disabled_folders;
        if (params.enable_memory !== undefined) postBody.enable_memory = params.enable_memory;
        if (params.summary_model_id) postBody.summary_model_id = params.summary_model_id;
        if (params.mcp_service_ids?.length) postBody.mcp_service_ids = params.mcp_service_ids;
        if (params.mentioned_items?.length) postBody.mentioned_items = params.mentioned_items;
        if (params.images?.length) postBody.images = params.images;
        if (params.attachment_uploads?.length) postBody.attachment_uploads = params.attachment_uploads;
        if (params.voice_metadata && Object.keys(params.voice_metadata).length > 0) postBody.voice_metadata = params.voice_metadata;
        postBody.channel = "web";
      }

      const streamHeaders: Record<string, string> = {
        "Content-Type": "application/json",
        ...(token ? { "Authorization": `Bearer ${token}` } : {}),
        ...(isIntegrationWidget ? { "X-CSRF-Token": getEmbeddedCSRFToken(), "Idempotency-Key": idempotencyKey } : {}),
        "Accept-Language": i18n.global.locale?.value || localStorage.getItem('locale') || 'zh-CN',
        "X-Request-ID": `${generateRandomString(12)}`,
        ...(tenantIdHeader ? { "X-Tenant-ID": tenantIdHeader } : {}),
      };
      
      await fetchEventSource(url, {
        method: params.method,
        headers: streamHeaders,
        credentials: isIntegrationWidget ? 'include' : 'same-origin',
        body:
          params.method == "POST"
            ? JSON.stringify(postBody)
            : null,
        signal: controller.signal,
        openWhenHidden: true,

        onopen: async (res) => {
          if (isIntegrationWidget && res.status === 410) {
            const payload = await res.json();
            const snapshotUrl = payload?.error?.message_snapshot_url;
            if (!snapshotUrl) throw new Error('SSE cursor expired without snapshot URL');
            const snapshot = await fetch(`${apiUrl}${snapshotUrl}`, {
              credentials: 'include',
              headers: token ? { Authorization: `Bearer ${token}` } : {},
            });
            if (!snapshot.ok) throw new Error(`snapshot recovery failed: ${snapshot.status}`);
            const body = await snapshot.json();
            chunkHandler?.({ response_type: body.data?.status === 'completed' ? 'complete' : 'error', id: body.data?.message?.id, content: body.data?.message?.content || '', done: true, data: { status: body.data?.status } });
            stopStream();
            return;
          }
          if (isIntegrationWidget && res.status === 403) {
            window.dispatchEvent(new CustomEvent('weknora:integration-authorization-changed'));
          }
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          isLoading.value = false;
        },

        onmessage: (ev) => {
          const raw = JSON.parse(ev.data);
          if (isIntegrationWidget && raw.event_id) {
            if (seenEventIds.has(raw.event_id)) return;
            seenEventIds.add(raw.event_id);
            lastEventId = raw.event_id;
            streamHeaders['Last-Event-ID'] = lastEventId;
          }
          const parsed = isIntegrationWidget ? mapIntegrationEvent(raw) : raw;
          if (!parsed) return;
          if (isTerminalStreamResponse(parsed)) receivedTerminalEvent = true;
          buffer.push(parsed); // 数据存入缓冲
          // 执行自定义处理
          if (chunkHandler) {
            chunkHandler(parsed);
          }
        },

        onerror: (err) => {
          if (isIntegrationWidget) {
            error.value = `${i18n.global.t('error.streamFailed')}: ${err}`;
            stopStream();
            throw err;
          }
          throw new Error(`${i18n.global.t('error.streamFailed')}: ${err}`);
        },

        onclose: () => {
          if (shouldReportUnexpectedStreamClose(receivedTerminalEvent, activeController.signal.aborted)) {
            error.value = i18n.global.t('error.streamFailed');
          }
          stopStream();
        },
      });
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
      stopStream()
    }
  }

  let chunkHandler: ((data: any) => void) | null = null
  // 注册块处理器
  const onChunk = (handler: () => void) => {
    chunkHandler = handler
  }


  // 停止流
  const stopStream = () => {
    controller.abort();
    controller = new AbortController(); // 重置控制器（如需重新发起）
    isStreaming.value = false;
    isLoading.value = false;
  }

  // 组件卸载时自动清理
  onUnmounted(stopStream)

  return {
    output,          // 显示内容
    isStreaming,     // 是否在流式传输中
    isLoading,       // 初始连接状态
    error,
    onChunk,
    startStream,     // 启动流
    stopStream       // 手动停止
  }
}
