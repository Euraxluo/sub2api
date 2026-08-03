export const LANDING_DEFAULTS: Record<string, string> = {
  site_name: 'Sub2API',
  site_subtitle: 'AI API Gateway Platform',
  site_logo: '',
  doc_url: '',
  eyebrow: '一个密钥，畅用多个 AI 模型',
  lede: '无需管理多个订阅账号，一站式接入 Claude、GPT、Gemini 等主流 AI 服务。统一路由、粘性会话、按量计费。',
  cta_primary: '立即开始',
  cta_secondary: '免费注册',
  login_text: '登录',
  tag1: '订阅转 API',
  tag2: '会话保持',
  tag3: '按量计费',
  bubble_user: '把 Claude / Codex 订阅转成 OpenAI 兼容接口',
  bubble_ai:
    '<strong>Ready.</strong> 已选好上游，并保持会话粘性。<code>POST /v1/chat/completions → 200 OK</code>',
  status: 'Upstream healthy',
  pain_title: '你是否也遇到这些问题？',
  pain_desc: '多订阅、多密钥、难管控——这正是网关要解决的日常摩擦。',
  features_title: '我们帮你解决',
  features_desc: '简单三步，开始省心使用 AI。',
  compare_title: '为什么选择我们？',
  compare_desc: '和官方单独订阅相比，网关更省、更稳、更好管。',
  providers_title: '已支持的 AI 模型',
  providers_desc: '一个 API，多种选择。',
  cta_title: '准备好开始了吗？',
  cta_desc: '注册即可获得免费试用额度，体验一站式 AI 服务。',
  cta_button: '免费注册',
  footer_note: 'Claude-inspired calm landing',
  accent: '',
}

export const LANDING_TEXT_FIELDS: Array<{
  key: string
  label: string
  type?: 'text' | 'textarea'
  span?: number
  placeholder?: string
}> = [
  { key: 'site_name', label: '站名' },
  { key: 'site_subtitle', label: '副标题' },
  { key: 'site_logo', label: 'Logo URL', span: 2 },
  { key: 'doc_url', label: '文档 URL', span: 2 },
  { key: 'eyebrow', label: '眉标', span: 2 },
  { key: 'lede', label: '介绍文案', type: 'textarea', span: 2 },
  { key: 'cta_primary', label: '主按钮' },
  { key: 'cta_secondary', label: '次按钮' },
  { key: 'login_text', label: '右上登录文案' },
  { key: 'accent', label: '强调色', placeholder: '#c96442' },
  { key: 'tag1', label: '标签 1' },
  { key: 'tag2', label: '标签 2' },
  { key: 'tag3', label: '标签 3' },
  { key: 'bubble_user', label: '对话-用户', type: 'textarea', span: 2 },
  { key: 'bubble_ai', label: '对话-AI (支持 HTML)', type: 'textarea', span: 2 },
  { key: 'status', label: '状态条' },
  { key: 'pain_title', label: '痛点标题' },
  { key: 'pain_desc', label: '痛点说明', type: 'textarea', span: 2 },
  { key: 'features_title', label: '方案标题' },
  { key: 'features_desc', label: '方案说明', type: 'textarea', span: 2 },
  { key: 'compare_title', label: '对比标题' },
  { key: 'compare_desc', label: '对比说明', type: 'textarea', span: 2 },
  { key: 'providers_title', label: '模型标题' },
  { key: 'providers_desc', label: '模型说明', type: 'textarea', span: 2 },
  { key: 'cta_title', label: '底栏标题' },
  { key: 'cta_desc', label: '底栏说明', type: 'textarea', span: 2 },
  { key: 'cta_button', label: '底栏按钮' },
  { key: 'footer_note', label: '页脚备注', span: 2 },
]

export const LANDING_HIDE_SECTIONS = [
  { id: 'pain', label: '痛点' },
  { id: 'features', label: '方案' },
  { id: 'compare', label: '对比' },
  { id: 'providers', label: '模型' },
  { id: 'cta', label: '底栏 CTA' },
] as const

export function createLandingParams(overrides: Record<string, string> = {}): Record<string, string> {
  return { ...LANDING_DEFAULTS, ...overrides }
}

/** Build a portable query string for sharing/debugging param sets. */
export function buildLandingQuery(
  params: Record<string, string>,
  hidden: string[],
): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    const trimmed = value.trim()
    if (trimmed === LANDING_DEFAULTS[key]) continue
    if (!trimmed && !LANDING_DEFAULTS[key]) continue
    search.set(key, trimmed)
  }
  if (hidden.length) search.set('hide', hidden.join(','))
  return search.toString()
}

export function parseLandingQuery(raw: string): {
  params: Record<string, string>
  hidden: string[]
} {
  const input = raw.trim()
  let query = input
  try {
    if (input.includes('://') || input.startsWith('/')) {
      const url = new URL(input, window.location.origin)
      query = url.search.startsWith('?') ? url.search.slice(1) : url.search
    } else if (input.startsWith('?')) {
      query = input.slice(1)
    }
  } catch {
    // treat as raw query
  }

  const search = new URLSearchParams(query)
  const params = createLandingParams()
  for (const key of Object.keys(LANDING_DEFAULTS)) {
    if (search.has(key)) params[key] = search.get(key) || ''
  }
  const hidden = (search.get('hide') || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  return { params, hidden }
}
