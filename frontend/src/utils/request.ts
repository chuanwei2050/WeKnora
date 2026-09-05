// src/utils/request.js
import axios, { type AxiosRequestConfig, type AxiosResponse } from "axios";
import { generateRandomString } from "./index";
import i18n from '@/i18n'
import { getApiBaseUrl } from './api-base';
import { getEmbeddedCSRFToken, getEmbeddedSessionToken, isCookieEmbeddedMode, clearEmbeddedAuth, notifyEmbeddedHost } from './embedded-runtime';

const t = (key: string) => i18n.global.t(key)

// API基础URL
const BASE_URL = getApiBaseUrl();


// 创建Axios实例
const instance = axios.create({
  baseURL: BASE_URL, // 使用配置的API基础URL
  timeout: 30000, // 请求超时时间
  headers: {
    "Content-Type": "application/json",
    "X-Request-ID": `${generateRandomString(12)}`,
  },
});

// 获取当前用户语言（用于 Accept-Language header）
function getCurrentLanguage(): string {
  return i18n.global.locale?.value || localStorage.getItem('locale') || 'zh-CN'
}


instance.interceptors.request.use(
  (config) => {
    const embedded = isCookieEmbeddedMode();
    config.withCredentials = embedded;
    // Embedded hosts may block third-party cookies; prefer in-memory session_token.
    const token = embedded ? getEmbeddedSessionToken() : localStorage.getItem('weknora_token');
    if (token) {
      config.headers["Authorization"] = `Bearer ${token}`;
    }
    
    // 添加用户语言偏好
    config.headers["Accept-Language"] = getCurrentLanguage();
    
    // 添加跨租户访问请求头（如果选择了其他租户）
    const selectedTenantId = embedded ? null : localStorage.getItem('weknora_selected_tenant_id');
    const defaultTenantId = localStorage.getItem('weknora_tenant');
    if (selectedTenantId && !config.url?.includes('/api/v1/admin/')) {
      try {
        const defaultTenant = defaultTenantId ? JSON.parse(defaultTenantId) : null;
        const defaultId = defaultTenant?.id ? String(defaultTenant.id) : null;
        // 如果选择的租户ID与默认租户ID不同，添加请求头
        if (selectedTenantId !== defaultId) {
          config.headers["X-Tenant-ID"] = selectedTenantId;
        }
      } catch (e) {
        console.error('Failed to parse tenant info', e);
      }
    }
    
    config.headers["X-Request-ID"] = `${generateRandomString(12)}`;
    if (embedded && !['get', 'head', 'options'].includes((config.method || 'get').toLowerCase())) {
      const csrf = getEmbeddedCSRFToken();
      if (csrf) config.headers['X-CSRF-Token'] = csrf;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Token刷新标志，防止多个请求同时刷新token
let isRefreshing = false;
let lastRefresh: { authorization: string; token: string } | null = null;
let failedQueue: Array<{ resolve: Function; reject: Function }> = [];

const PUBLIC_AUTH_PATHS = ['/auth/auto-setup', '/auth/login', '/auth/register', '/auth/oidc/'];

function isPublicAuthRequest(url?: string): boolean {
  if (!url) return false;
  return PUBLIC_AUTH_PATHS.some(p => url.includes(p));
}

// 处理队列中的请求
const processQueue = (error: any, token: string | null = null) => {
  failedQueue.forEach(({ resolve, reject }) => {
    if (error) {
      reject(error);
    } else {
      resolve(token);
    }
  });
  
  failedQueue = [];
};

function redirectToLogin() {
  if (typeof window === 'undefined') return;
  const loginPath = window.location.pathname.startsWith('/knowledge') ? '/knowledge/login' : '/login';
  if (window.location.pathname === loginPath) return;
  window.location.href = loginPath;
}

instance.interceptors.response.use(
  (response) => {
    // 根据业务状态码处理逻辑
    const { status, data } = response;
    if (status >= 200 && status < 300) {
      return response;
    } else {
      return Promise.reject(data);
    }
  },
  async (error: any) => {
    const originalRequest = error.config;
    if (error.response?.status === 401 && isCookieEmbeddedMode()) {
      clearEmbeddedAuth();
      notifyEmbeddedHost('unauthorized');
      return Promise.reject({ status: 401, message: t('error.pleaseRelogin') });
    }
    
    if (!error.response) {
      return Promise.reject({ message: t('error.networkError') });
    }
    
    // 公开接口（auto-setup / login / register / oidc）的 401 不走 refresh 逻辑，直接返回错误
    if (error.response.status === 401 && isPublicAuthRequest(originalRequest?.url)) {
      const { status, data } = error.response;
      return Promise.reject({ status, message: (typeof data === 'object' ? (data?.error?.message || data?.message) : data) || t('error.invalidCredentials') });
    }

    // A request started before login (or with an older token) may finish after
    // the new session has already been persisted. It must not refresh or clear
    // the current session.
    if (error.response.status === 401) {
      const requestAuthorization = originalRequest?.headers?.Authorization || originalRequest?.headers?.get?.('Authorization')
      const currentToken = localStorage.getItem('weknora_token')
      if (currentToken && requestAuthorization !== `Bearer ${currentToken}` && !originalRequest._retry
        && lastRefresh !== null && lastRefresh.authorization === requestAuthorization && lastRefresh.token === currentToken) {
        originalRequest._retry = true
        originalRequest.headers['Authorization'] = `Bearer ${currentToken}`
        return instance(originalRequest)
      }
      if (!requestAuthorization || (currentToken && requestAuthorization !== `Bearer ${currentToken}`)) {
        if (!currentToken) redirectToLogin()
        const data = error.response.data
        return Promise.reject({
          status: 401,
          message: (typeof data === 'object' ? (data?.error?.message || data?.message || data?.error) : data) || t('error.pleaseRelogin')
        })
      }
    }

    // 如果是401错误且不是刷新token的请求，尝试刷新token
    if (error.response.status === 401 && !originalRequest._retry && !originalRequest.url?.includes('/auth/refresh')) {
      if (isRefreshing) {
        // 如果正在刷新token，将请求加入队列
        return new Promise((resolve, reject) => {
          failedQueue.push({ resolve, reject });
        }).then(token => {
          originalRequest._retry = true;
          originalRequest.headers['Authorization'] = 'Bearer ' + token;
          return instance(originalRequest);
        }).catch(err => {
          return Promise.reject(err);
        });
      }
      
      originalRequest._retry = true;
      isRefreshing = true;
      
      const refreshToken = localStorage.getItem('weknora_refresh_token');
      
      if (refreshToken) {
        try {
          // 动态导入refresh token API
          const { refreshToken: refreshTokenAPI } = await import('../api/auth/index');
          const response = await refreshTokenAPI(refreshToken);
          
          if (response.success && response.data) {
            const { token, refreshToken: newRefreshToken } = response.data;
            
            // Do not apply an old refresh response over a newly logged-in session.
            if (localStorage.getItem('weknora_refresh_token') !== refreshToken) {
              const error = { status: 401, message: t('error.pleaseRelogin') };
              processQueue(error);
              return Promise.reject(error);
            }
            lastRefresh = { authorization: originalRequest.headers['Authorization'], token };
            // 更新localStorage中的token
            localStorage.setItem('weknora_token', token);
            localStorage.setItem('weknora_refresh_token', newRefreshToken);
            
            // 更新请求头
            originalRequest.headers['Authorization'] = 'Bearer ' + token;
            
            // 处理队列中的请求
            processQueue(null, token);
            
            return instance(originalRequest);
          } else {
            throw new Error(response.message || t('error.tokenRefreshFailed'));
          }
        } catch (refreshError) {
          // A failed refresh from an older login must not clear the current login.
          if (localStorage.getItem('weknora_refresh_token') !== refreshToken) {
            processQueue(refreshError);
            return Promise.reject(refreshError);
          }
          // 刷新失败，清除所有token并跳转到登录页
          localStorage.removeItem('weknora_token');
          localStorage.removeItem('weknora_refresh_token');
          localStorage.removeItem('weknora_user');
          localStorage.removeItem('weknora_tenant');
          
          processQueue(refreshError, null);
          
          redirectToLogin();
          
          return Promise.reject(refreshError);
        } finally {
          isRefreshing = false;
        }
      } else {
        // 没有refresh token，结束刷新状态并拒绝等待中的请求
        isRefreshing = false;
        processQueue({ status: 401, message: t('error.pleaseRelogin') });
        localStorage.removeItem('weknora_token');
        localStorage.removeItem('weknora_user');
        localStorage.removeItem('weknora_tenant');
        
        redirectToLogin();
        
        return Promise.reject({ message: t('error.pleaseRelogin') });
      }
    }
    
    // 处理 Nginx 413 Request Entity Too Large
    if (error.response.status === 413) {
      return Promise.reject({ 
        status: 413, 
        message: t('error.fileSizeExceeded'),
        success: false
      });
    }

    const { status, data } = error.response;
    // 将HTTP状态码一并抛出，方便上层判断401等场景
    // 后端返回格式: { success: false, error: { code, message, details } }
    // 提取 error.message 作为顶层 message，方便前端使用 error?.message 获取
    let errorMessage: string | undefined;
    if (typeof data === 'object') {
      if (typeof data?.error === 'string') {
        errorMessage = data.error;
      } else if (data?.error?.message) {
        errorMessage = data.error.message;
      } else {
        errorMessage = data?.message;
      }
    } else if (typeof data === 'string') {
      errorMessage = data;
    }
    return Promise.reject({ 
      status, 
      message: errorMessage,
      ...(typeof data === 'object' ? data : {}) 
    });
  }
);

function unwrapResponse<T>(request: Promise<AxiosResponse<T>>): Promise<T> {
  return request.then(response => response.data);
}

export function get<T = any>(url: string, config?: AxiosRequestConfig): Promise<T> {
  return unwrapResponse(instance.get<T>(url, config));
}

export async function getDown<T = Blob>(url: string, config?: AxiosRequestConfig): Promise<T> {
  return unwrapResponse(instance.get<T>(url, {
    ...config,
    responseType: "blob",
  }));
}

export function postUpload<T = any>(url: string, data = {}, onUploadProgress?: (progressEvent: any) => void): Promise<T> {
  return unwrapResponse(instance.post<T>(url, data, {
    timeout: 0,
    headers: {
      "Content-Type": "multipart/form-data",
      "X-Request-ID": `${generateRandomString(12)}`,
    },
    onUploadProgress,
  }));
}

export function postChat<T = any>(url: string, data = {}): Promise<T> {
  return unwrapResponse(instance.post<T>(url, data, {
    headers: {
      "Content-Type": "text/event-stream;charset=utf-8",
      "X-Request-ID": `${generateRandomString(12)}`,
    },
  }));
}

export function post<T = any>(url: string, data = {}, config?: any): Promise<T> {
  return unwrapResponse(instance.post<T>(url, data, config));
}

export function put<T = any>(url: string, data = {}): Promise<T> {
  return unwrapResponse(instance.put<T>(url, data));
}

export function patch<T = any>(url: string, data = {}): Promise<T> {
  return unwrapResponse(instance.patch<T>(url, data));
}

export function del<T = any>(url: string, data?: any): Promise<T> {
  return unwrapResponse(instance.delete<T>(url, { data }));
}
