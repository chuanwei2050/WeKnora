import axios, { AxiosError, type AxiosAdapter } from 'axios'
import { beforeEach, expect, it, vi } from 'vitest'

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn() }))
vi.mock('../src/api/auth/index', () => ({ refreshToken: refresh }))
vi.mock('../src/i18n', () => ({ default: { global: { t: (key: string) => key, locale: { value: 'zh-CN' } } } }))
vi.mock('../src/utils/index', () => ({ generateRandomString: () => 'request-id' }))

async function client(adapter: AxiosAdapter) {
  const instance = axios.create({ adapter })
  const create = vi.spyOn(axios, 'create').mockReturnValue(instance)
  const request = await import('../src/utils/request')
  create.mockRestore()
  return request
}

function unauthorized(config: Parameters<AxiosAdapter>[0]): never {
  throw new AxiosError('Unauthorized', 'ERR_BAD_REQUEST', config, undefined, {
    config, status: 401, statusText: 'Unauthorized', headers: {}, data: { message: 'expired' },
  })
}

beforeEach(() => {
  vi.resetModules()
  refresh.mockReset()
  localStorage.clear()
  localStorage.setItem('weknora_token', 'expired')
  localStorage.setItem('weknora_refresh_token', 'refresh')
})

it('unwraps the refresh leader and queued responses exactly once', async () => {
  const payload = { success: true, data: { profile: 'online' } }
  let failures = 0
  let allFailed = () => {}
  const failed = new Promise<void>(resolve => { allFailed = resolve })
  refresh.mockImplementation(async () => {
    await failed
    return { success: true, data: { token: 'renewed', refreshToken: 'next' } }
  })
  const { get } = await client(async config => {
    if (config.headers.Authorization !== 'Bearer renewed') {
      if (++failures === 2) allFailed()
      unauthorized(config)
    }
    return { config, status: 200, statusText: 'OK', headers: {}, data: payload }
  })
  expect(await Promise.all([get('/models'), get('/profile')])).toEqual([payload, payload])
  expect(refresh).toHaveBeenCalledTimes(1)
})

it('does not replay an old request under a different login', async () => {
  let calls = 0
  const { get } = await client(async config => {
    if (++calls === 1) {
      localStorage.setItem('weknora_token', 'new-login')
      unauthorized(config)
    }
    expect(config.headers.Authorization).toBe('Bearer new-login')
    return { config, status: 200, statusText: 'OK', headers: {}, data: { success: true } }
  })
  await expect(get('/models')).rejects.toMatchObject({ status: 401 })
  expect(localStorage.getItem('weknora_token')).toBe('new-login')
  expect(calls).toBe(1)
  expect(refresh).not.toHaveBeenCalled()
})

it('releases the refresh lock when no refresh token exists', async () => {
  window.history.replaceState(null, '', '/login')
  localStorage.removeItem('weknora_refresh_token')
  const { get } = await client(async config => {
    if (config.headers.Authorization !== 'Bearer renewed') unauthorized(config)
    return { config, status: 200, statusText: 'OK', headers: {}, data: { success: true } }
  })
  await expect(get('/models')).rejects.toBeDefined()
  localStorage.setItem('weknora_token', 'expired-again')
  localStorage.setItem('weknora_refresh_token', 'refresh-again')
  refresh.mockResolvedValue({ success: true, data: { token: 'renewed', refreshToken: 'next' } })
  await expect(get('/models')).resolves.toEqual({ success: true })
  expect(refresh).toHaveBeenCalledTimes(1)
})


it('retries a late 401 from the same refresh without another refresh call', async () => {
  let releaseLate = () => {}
  const late = new Promise<void>(resolve => { releaseLate = resolve })
  refresh.mockResolvedValue({ success: true, data: { token: 'renewed', refreshToken: 'next' } })
  const { get } = await client(async config => {
    if (config.headers.Authorization !== 'Bearer renewed') {
      if (config.url === '/late') await late
      unauthorized(config)
    }
    releaseLate()
    return { config, status: 200, statusText: 'OK', headers: {}, data: { success: true } }
  })
  await expect(Promise.all([get('/leader'), get('/late')])).resolves.toEqual([{ success: true }, { success: true }])
  expect(refresh).toHaveBeenCalledTimes(1)
})

it.each([true, false])('preserves a new login when an older refresh completes (success=%s)', async success => {
  refresh.mockImplementation(async () => {
    localStorage.setItem('weknora_token', 'new-login')
    localStorage.setItem('weknora_refresh_token', 'new-login-refresh')
    return success ? { success: true, data: { token: 'obsolete', refreshToken: 'obsolete-refresh' } } : { success: false, message: 'expired' }
  })
  const { get } = await client(async config => unauthorized(config))
  await expect(get('/models')).rejects.toBeDefined()
  expect(localStorage.getItem('weknora_token')).toBe('new-login')
  expect(localStorage.getItem('weknora_refresh_token')).toBe('new-login-refresh')
})
